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
	"gopkg.in/zorkian/go-datadog-api.v2"
)

type ConcourseConfig struct {
	Username string        `json:"username"`
	Password string        `json:"password"`
	URL      string        `json:"url"`
	Team     string        `json:"team"`
	Target   rc.TargetName `json:"target"`
}

type DatadogConfig struct {
	APIKey       string `json:"api-key"`
	APPKey       string `json:"app-key"`
	MetricPrefix string `json:"metric-prefix"`
}

type Config struct {
	Concourse ConcourseConfig `json:"concourse"`
	Datadog   DatadogConfig   `json:"datadog"`
}

type TaskMetric struct {
	ID        string
	Name      string
	StartTime int64
	EndTime   int64

	Type string
	Tags []string
}

func (em *TaskMetric) UpdateTime(timestamp int64) {
	if em.StartTime == 0 || timestamp < em.StartTime {
		em.StartTime = timestamp
	}

	if em.EndTime == 0 || timestamp > em.EndTime {
		em.EndTime = timestamp
	}
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
	target, err := rc.NewBasicAuthTarget(config.Concourse.Target, config.Concourse.URL, config.Concourse.Team, false, config.Concourse.Username, config.Concourse.Password, "", false)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	token, err := target.Team().AuthToken()
	if err != nil {
		fmt.Println(err.Error())

		os.Exit(1)
	}

	err = rc.SaveTarget(config.Concourse.Target, config.Concourse.URL, true, config.Concourse.Team, &rc.TargetToken{Type: token.Type, Value: token.Value}, "")

	target, err = rc.LoadTarget(config.Concourse.Target, false)
	if err != nil {
		fmt.Println(err.Error())

		os.Exit(1)
	}

	return target
}

func FilterBuilds(team concourse.Team, timeAgo time.Time) []atc.Build {
	pipelineName := "rabbitmq-1.11" // TODO: Remove this

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

func CollectIDs(plan atc.Plan, ids map[string]TaskMetric) {
	if plan.Aggregate != nil {
		for _, p1 := range *plan.Aggregate {
			CollectIDs(p1, ids)
		}
	}

	if plan.Do != nil {
		for _, p1 := range *plan.Do {
			CollectIDs(p1, ids)
		}
	}

	if plan.Retry != nil {
		for _, p1 := range *plan.Retry {
			CollectIDs(p1, ids)
		}
	}

	if plan.OnSuccess != nil {
		CollectIDs(plan.OnSuccess.Step, ids)
		CollectIDs(plan.OnSuccess.Next, ids)
	}

	if plan.OnFailure != nil {
		CollectIDs(plan.OnFailure.Step, ids)
		CollectIDs(plan.OnFailure.Next, ids)
	}

	if plan.OnAbort != nil {
		CollectIDs(plan.OnAbort.Step, ids)
		CollectIDs(plan.OnAbort.Next, ids)
	}

	if plan.Ensure != nil {
		CollectIDs(plan.Ensure.Step, ids)
		CollectIDs(plan.Ensure.Next, ids)
	}

	if plan.Try != nil {
		CollectIDs(plan.Try.Step, ids)
	}

	if plan.Timeout != nil {
		CollectIDs(plan.Timeout.Step, ids)
	}

	planID := string(plan.ID)

	if plan.Task != nil {
		ids[planID] = TaskMetric{
			ID:   planID,
			Name: plan.Task.Name,
			Type: "task",
		}
	}

	if plan.Get != nil {
		ids[planID] = TaskMetric{
			ID:   string(planID),
			Name: plan.Get.Name,
			Type: "get",
		}
	}

	if plan.Put != nil {
		ids[planID] = TaskMetric{
			ID:   string(planID),
			Name: plan.Put.Name,
			Type: "put",
		}
	}
}

func CollectEventTimestamps(events []atc.Event, ids map[string]TaskMetric) {
	//event.Error{}
	//event.FinishTask{} +1
	//event.InitializeTask{}
	//event.StartTask{} +1 // sets the timestamp to 0 always
	//event.Log{} +1
	//event.FinishGet{}
	//event.FinishPut{}

	for _, currentEvent := range events {
		switch e := currentEvent.(type) {
		case event.Log:
			originID := string(e.Origin.ID)

			currentMetric := ids[originID]
			currentMetric.UpdateTime(e.Time)
			ids[originID] = currentMetric

		default:
			// Skipping
		}
	}

}

func PrettyPrint(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	println(string(b))
}

func GetMetrics(client concourse.Client, builds []atc.Build) map[string]TaskMetric {
	ids := map[string]TaskMetric{}

	for _, build := range builds {

		buildPublicPlan, _, _ := client.BuildPlan(build.ID) // TODO: Check error

		var buildPlan atc.Plan
		err := json.Unmarshal(*buildPublicPlan.Plan, &buildPlan)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		CollectIDs(buildPlan, ids)

		// Start getting buildEvents
		buildEvents, err := client.BuildEvents(strconv.Itoa(build.ID))
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		// https://github.com/concourse/fly/blob/03cd3175e8c409f35d107c201f0068757d920311/eventstream/render.go

		var events []atc.Event
		for {
			streamEvent, err := buildEvents.NextEvent()
			if err != nil {
				if err == io.EOF {
					break
				}

				fmt.Println(err.Error())
				os.Exit(1)
			}

			events = append(events, streamEvent)
		}

		CollectEventTimestamps(events, ids)

		for key, value := range ids {
			value.Tags = []string{
				fmt.Sprintf("job-name:%s", build.JobName),
				fmt.Sprintf("build-name:%s", build.Name),
				fmt.Sprintf("build-id:%d", build.ID),
				fmt.Sprintf("build-status:%s", build.Status),
				fmt.Sprintf("pipeline-name:%s", build.PipelineName),
				fmt.Sprintf("team-name:%s", build.TeamName),
				fmt.Sprintf("task-type:%s", value.Type),
				fmt.Sprintf("task-id:%s", value.ID),
			}

			ids[key] = value
		}
	}

	return ids
}

func PublishMetrics(datadogConfig DatadogConfig, taskMetrics map[string]TaskMetric) {
	datadogClient := datadog.NewClient(datadogConfig.APIKey, datadogConfig.APPKey)

	metricName := fmt.Sprintf("%s.tasks", datadogConfig.MetricPrefix)

	var metrics []datadog.Metric

	for _, value := range taskMetrics {
		metric := datadog.Metric{}
		metric.SetMetric(metricName)
		metric.SetUnit("")
		metric.Tags = value.Tags

		taskDuration := time.Unix(value.EndTime, 0).Sub(time.Unix(value.StartTime, 0))

		metric.Points = append(metric.Points, datadog.DataPoint{float64(value.EndTime), taskDuration.Minutes()})

		metrics = append(metrics, metric)
	}

	datadogClient.PostMetrics(metrics)
}

func main() {
	configPath := flag.String("config-path", "", "help message for flagname")
	flag.Parse()

	minusOneHour, _ := time.ParseDuration("-1h")
	oneDayAgo := time.Now().Add(24 * minusOneHour)
	fmt.Println(oneDayAgo)

	fmt.Printf("Reading config from: %s\n", *configPath)
	config := ReadConfig(*configPath)

	target := GetTarget(config)

	builds := FilterBuilds(target.Team(), oneDayAgo)

	metrics := GetMetrics(target.Client(), builds)

	PublishMetrics(config.Datadog, metrics)
}
