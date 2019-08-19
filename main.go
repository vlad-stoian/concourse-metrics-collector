package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/concourse/concourse/fly/commands"

	"github.com/pkg/errors"

	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/event"
	"github.com/concourse/concourse/fly/rc"
	"github.com/concourse/concourse/go-concourse/concourse"
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
	ID   string `json:"id"`
	Name string `json:"name"`

	InitializeTime int64 `json:"initialize_time"`
	StartTime      int64 `json:"start_time"`
	FinishTime     int64 `json:"finish_time"`

	Type string `json:"type"`

	PipelineName string `json:"pipeline_name"`
	JobName      string `json:"job_name"`
	BuildName    string `json:"build_name"`
	TeamName     string `json:"team_name"`
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

func FilterBuilds(team concourse.Team, timeAgo time.Time) []atc.Build {
	var builds []atc.Build

	pipelines, _ := team.ListPipelines()
	for _, pipeline := range pipelines {

		jobs, _ := team.ListJobs(pipeline.Name)

		for _, job := range jobs {
			page := concourse.Page{Limit: 0}
			jobBuilds, _, _, _ := team.JobBuilds(pipeline.Name, job.Name, page)

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
	}

	return builds
}

func CollectIDs(plan atc.Plan, ids map[string]TaskMetric) {
	if plan.Aggregate != nil {
		for _, p1 := range *plan.Aggregate {
			CollectIDs(p1, ids)
		}
	}

	if plan.InParallel != nil {
		for _, p1 := range plan.InParallel.Steps {
			CollectIDs(p1, ids)
		}
	}

	if plan.Do != nil {
		for _, p1 := range *plan.Do {
			CollectIDs(p1, ids)
		}
	}

	if plan.OnAbort != nil {
		CollectIDs(plan.OnAbort.Step, ids)
		CollectIDs(plan.OnAbort.Next, ids)
	}

	if plan.OnError != nil {
		CollectIDs(plan.OnError.Step, ids)
		CollectIDs(plan.OnError.Next, ids)
	}

	if plan.Ensure != nil {
		CollectIDs(plan.Ensure.Step, ids)
		CollectIDs(plan.Ensure.Next, ids)
	}

	if plan.OnSuccess != nil {
		CollectIDs(plan.OnSuccess.Step, ids)
		CollectIDs(plan.OnSuccess.Next, ids)
	}

	if plan.OnFailure != nil {
		CollectIDs(plan.OnFailure.Step, ids)
		CollectIDs(plan.OnFailure.Next, ids)
	}

	if plan.Try != nil {
		CollectIDs(plan.Try.Step, ids)
	}

	if plan.Timeout != nil {
		CollectIDs(plan.Timeout.Step, ids)
	}

	if plan.Retry != nil {
		for _, p1 := range *plan.Retry {
			CollectIDs(p1, ids)
		}
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
			ID:   planID,
			Name: plan.Get.Name,
			Type: "get",
		}
	}

	if plan.Put != nil {
		ids[planID] = TaskMetric{
			ID:   planID,
			Name: plan.Put.Name,
			Type: "put",
		}
	}
}

func PrettyPrint(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func UglyPrint(v interface{}) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

type GenericEvent struct {
	Origin struct {
		ID string `json:"id"`
	} `json:"origin"`
	Time int64 `json:"time"`
}

func GetOriginAndTime(event atc.Event) (string, int64, error) {
	eventBytes, err := json.Marshal(event)
	if err != nil {
		return "", 0, errors.Wrap(err, "event marshaling failed")
	}

	var genericEvent GenericEvent
	err = json.Unmarshal(eventBytes, &genericEvent)
	if err != nil {
		return "", 0, errors.Wrap(err, "event unmarshaling failed")
	}

	return genericEvent.Origin.ID, genericEvent.Time, nil
}

func GetMetrics(client concourse.Client, builds []atc.Build) (map[string]TaskMetric, error) {
	allTaskMetrics := map[string]TaskMetric{}

	for _, build := range builds {

		_, _ = fmt.Fprintf(os.Stderr, "Build %d, unix %d, time %s\n", build.ID, build.StartTime, time.Unix(build.StartTime, 0))

		someTaskMetrics := map[string]TaskMetric{}

		buildPublicPlan, _, err := client.BuildPlan(build.ID) // TODO: Check error
		if err != nil {
			return nil, errors.Wrap(err, "failed to get build plan")
		}

		var buildPlan atc.Plan
		err = json.Unmarshal(*buildPublicPlan.Plan, &buildPlan)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal build plan")
		}

		CollectIDs(buildPlan, someTaskMetrics)

		buildEvents, err := client.BuildEvents(strconv.Itoa(build.ID))
		if err != nil {
			return nil, errors.Wrap(err, "failed to get build events")
		}

		// https://github.com/concourse/fly/blob/03cd3175e8c409f35d107c201f0068757d920311/eventstream/render.go

		var events []atc.Event
		for {
			streamEvent, err := buildEvents.NextEvent()
			if err != nil {
				if err == io.EOF {
					break
				}

				return nil, errors.Wrap(err, "failed to get the next event")
			}

			events = append(events, streamEvent)
		}

		initializeTimes := map[string]int64{}
		startTimes := map[string]int64{}
		finishTimes := map[string]int64{}

		for _, currentEvent := range events {
			if strings.HasPrefix(string(currentEvent.EventType()), "initialize-") {
				originID, timez, err := GetOriginAndTime(currentEvent)
				if err != nil {
					return nil, errors.Wrap(err, "failed to get origin and time for an initialize- event")
				}
				initializeTimes[originID] = timez
			}

			if strings.HasPrefix(string(currentEvent.EventType()), "start-") {
				originID, timez, err := GetOriginAndTime(currentEvent)
				if err != nil {
					return nil, errors.Wrap(err, "failed to get origin and time for an start- event")
				}
				startTimes[originID] = timez
			}

			if strings.HasPrefix(string(currentEvent.EventType()), "finish-") {
				originID, timez, err := GetOriginAndTime(currentEvent)
				if err != nil {
					return nil, errors.Wrap(err, "failed to get origin and time for an finish- event")
				}
				finishTimes[originID] = timez
			}
		}

		for _, value := range someTaskMetrics {
			value.JobName = build.JobName
			value.PipelineName = build.PipelineName
			value.BuildName = build.Name
			value.TeamName = build.TeamName

			value.InitializeTime = initializeTimes[value.ID]
			value.StartTime = startTimes[value.ID]
			value.FinishTime = finishTimes[value.ID]

			UglyPrint(value)
		}
	}

	return allTaskMetrics, nil
}

func GetToken(config Config) (rc.Target, error) {
	target, err := rc.LoadTarget(config.Concourse.Target, false)
	if err != nil {
		return nil, errors.Wrap(err, "load target failed")
	}

	err = target.Validate()

	if err != nil {
		if strings.HasSuffix(err.Error(), "not authorized") {
			commands.Fly.Target = config.Concourse.Target
			login := &commands.LoginCommand{BrowserOnly: true}
			err := login.Execute([]string{})
			if err != nil {
				return nil, errors.Wrap(err, "login command failed")
			}
		} else {
			return nil, errors.Wrap(err, "validate failed")
		}
	}

	return target, nil
}

func main() {
	configPath := flag.String("config-path", "", "help message for flagname")
	flag.Parse()

	logger := lager.NewLogger("blackbox")
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.DEBUG))

	//minusOneHour, _ := time.ParseDuration("-200h")
	//oneHourAgo := time.Now().Add(minusOneHour)
	//fmt.Println(oneHourAgo)

	logger.Debug("reading-config", lager.Data{"configPath": *configPath})
	config := ReadConfig(*configPath)

	logger.Debug("get-token")
	target, err := GetToken(config)
	if err != nil {
		logger.Fatal("get-token-failed", err)
	}

	// Should be fixed once this gets merged: https://github.com/concourse/concourse/pull/4291
	event.RegisterEvent(event.InitializeGet{})
	event.RegisterEvent(event.InitializePut{})

	logger.Debug("filtering-builds")
	builds := FilterBuilds(target.Team(), time.Unix(0, 0))

	logger.Debug("getting-metrics")
	_, err = GetMetrics(target.Client(), builds)
	if err != nil {
		logger.Fatal("get-metrics-failed", err)
	}
}
