package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/atc/event"

	"github.com/concourse/fly/rc"
	"github.com/concourse/go-concourse/concourse"
)

type Concourse struct {
	Username string `json:"username"`
	Password string `json:"password"`
	URL      string `json:"url"`
}

type Config struct {
	Concourse Concourse `json:"concourse"`
}

func ReadConfig(configPath string) Config {
	raw, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	var config Config
	err = json.Unmarshal(raw, &config)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	return config
}

func GetTarget(config Config) rc.Target {
	target, err := rc.NewBasicAuthTarget("rabbit", config.Concourse.URL, "main", false, config.Concourse.Username, config.Concourse.Password, "", false)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	token, err := target.Team().AuthToken()
	if err != nil {
		fmt.Println(err.Error())

		os.Exit(1)
	}

	err = rc.SaveTarget(
		"rabbit",
		config.Concourse.URL,
		true,
		"main",
		&rc.TargetToken{
			Type:  token.Type,
			Value: token.Value,
		},
		"",
	)

	target, err = rc.LoadTarget("rabbit", false)
	if err != nil {
		fmt.Println(err.Error())

		os.Exit(1)
	}

	return target
}

func FilterBuilds(team concourse.Team, timeAgo time.Time) []atc.Build {
	pipelineName := "rabbitmq-1.11"

	jobs, _ := team.ListJobs(pipelineName)

	var builds []atc.Build

	for _, job := range jobs {
		page := concourse.Page{Limit: 0}
		jobBuilds, _, _, _ := team.JobBuilds(pipelineName, job.Name, page)

		for _, jobBuild := range jobBuilds {
			buildTime := time.Unix(jobBuild.EndTime, 0)

			if jobBuild.IsRunning() || jobBuild.OneOff() {
				continue
			}

			if buildTime.After(timeAgo) {
				builds = append(builds, jobBuild)
			}
		}
	}

	return builds
}

func PrintMetrics(client concourse.Client, builds []atc.Build) {
	for _, build := range builds {
		events, err := client.BuildEvents(strconv.Itoa(build.ID))
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		fmt.Println(build.APIURL)

		// var times map[string]atc.Build

		// https://github.com/concourse/fly/blob/03cd3175e8c409f35d107c201f0068757d920311/eventstream/render.go
		for {
			streamEvent, err := events.NextEvent()
			if err != nil {
				if err == io.EOF {
					break
				}

				fmt.Println(err.Error())
				os.Exit(1)
			}

			switch e := streamEvent.(type) {
			case event.StartTask:
				fmt.Println("------------")
				fmt.Println(e)
				fmt.Println("------------")
			case event.FinishTask:
				fmt.Println(e)
			default:
			}
		}

		os.Exit(0)
	}

}

func main() {
	configPath := flag.String("config-path", "", "help message for flagname")
	flag.Parse()

	minusOneHour, _ := time.ParseDuration("-1h")
	oneWeekAgo := time.Now().Add(7 * 24 * minusOneHour)
	fmt.Println(oneWeekAgo)

	fmt.Printf("Reading config from: %s\n", *configPath)
	config := ReadConfig(*configPath)

	target := GetTarget(config)

	builds := FilterBuilds(target.Team(), oneWeekAgo)

	PrintMetrics(target.Client(), builds)

}
