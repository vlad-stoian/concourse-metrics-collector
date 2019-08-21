package main

import (
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/event"
	"github.com/concourse/concourse/fly/commands"
	"github.com/concourse/concourse/fly/rc"
	"github.com/concourse/concourse/go-concourse/concourse"
	"github.com/pkg/errors"
	"github.com/vlad-stoian/concourse-metrics-collector/internal/pkg/cache"
	"github.com/vlad-stoian/concourse-metrics-collector/internal/pkg/models"
	"github.com/vlad-stoian/concourse-metrics-collector/internal/pkg/utils"
)

type ConcourseConfig struct {
	Username string        `json:"username"`
	Password string        `json:"password"`
	URL      string        `json:"url"`
	Team     string        `json:"team"`
	Target   rc.TargetName `json:"target"`
}

type Config struct {
	Concourse ConcourseConfig `json:"concourse"`
}

func ReadConfig(configPath string) (Config, error) {
	raw, err := ioutil.ReadFile(configPath)
	if err != nil {
		return Config{}, errors.Wrap(err, "failed-to-read-config-file")
	}

	var config Config
	err = json.Unmarshal(raw, &config)
	if err != nil {
		return Config{}, errors.Wrap(err, "failed-to-unmarshal-config")
	}

	return config, nil
}

func GetMetrics(client concourse.Client, build atc.Build) (models.BuildMetric, error) {
	taskMetrics := map[string]models.TaskMetric{}

	buildPublicPlan, found, err := client.BuildPlan(build.ID)
	if err != nil {
		return models.BuildMetric{}, errors.Wrap(err, "failed to get build plan")
	}

	if !found {
		return models.BuildMetric{}, nil
	}

	var buildPlan atc.Plan
	err = json.Unmarshal(*buildPublicPlan.Plan, &buildPlan)
	if err != nil {
		return models.BuildMetric{}, errors.Wrap(err, "failed to unmarshal build plan")
	}

	utils.CollectIDs(buildPlan, taskMetrics)

	buildEvents, err := client.BuildEvents(strconv.Itoa(build.ID))
	if err != nil {
		return models.BuildMetric{}, errors.Wrap(err, "failed to get build events")
	}

	// https://github.com/concourse/concourse/blob/09aecaa35913a78a475f72abdb33783903fa3f3b/fly/eventstream/render.go
	var events []atc.Event
	for {
		streamEvent, err := buildEvents.NextEvent()
		if err != nil {
			if err == io.EOF {
				break
			}

			return models.BuildMetric{}, errors.Wrap(err, "failed to get the next event")
		}

		events = append(events, streamEvent)
	}

	initializeTimes := map[string]int64{}
	startTimes := map[string]int64{}
	finishTimes := map[string]int64{}

	for _, currentEvent := range events {
		if strings.HasPrefix(string(currentEvent.EventType()), "initialize-") {
			originID, timez, err := utils.GetOriginAndTime(currentEvent)
			if err != nil {
				return models.BuildMetric{}, errors.Wrap(err, "failed to get origin and time for an initialize- event")
			}
			initializeTimes[originID] = timez
		}

		if strings.HasPrefix(string(currentEvent.EventType()), "start-") {
			originID, timez, err := utils.GetOriginAndTime(currentEvent)
			if err != nil {
				return models.BuildMetric{}, errors.Wrap(err, "failed to get origin and time for an start- event")
			}
			startTimes[originID] = timez
		}

		if strings.HasPrefix(string(currentEvent.EventType()), "finish-") {
			originID, timez, err := utils.GetOriginAndTime(currentEvent)
			if err != nil {
				return models.BuildMetric{}, errors.Wrap(err, "failed to get origin and time for an finish- event")
			}
			finishTimes[originID] = timez
		}
	}

	var allTaskMetrics []models.TaskMetric
	for _, value := range taskMetrics {
		value.BuildId = build.ID

		value.InitializeTime = initializeTimes[value.ID]
		value.StartTime = startTimes[value.ID]
		value.FinishTime = finishTimes[value.ID]

		allTaskMetrics = append(allTaskMetrics, value)
	}

	return models.BuildMetric{
		ID:        build.ID,
		Name:      build.Name,
		Status:    build.Status,
		StartTime: build.StartTime,
		EndTime:   build.EndTime,

		TeamName:     build.TeamName,
		PipelineName: build.PipelineName,
		JobName:      build.JobName,
		Tasks:        allTaskMetrics,
	}, nil
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
	configPath := flag.String("config-path", "", "please provide the path to your config file")
	cachePath := flag.String("cache-path", "", "please provide the path to your cache file")
	flag.Parse()

	logger := lager.NewLogger("blackbox")
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.DEBUG))

	//someTimeAgo := time.Now().AddDate(0, -3, 0)
	//logger.Debug("some-time-ago", lager.Data{"someTimeAgo": someTimeAgo})

	logger.Debug("reading-config", lager.Data{"configPath": *configPath})
	config, err := ReadConfig(*configPath)
	if err != nil {
		logger.Fatal("reading-config-failed", err)
	}

	logger.Debug("reading-cache", lager.Data{"cachePath": *cachePath})
	cacher, err := cache.NewCache(*cachePath)
	if err != nil {
		logger.Fatal("reading-cache-failed", err)
	}

	logger.Debug("get-token")
	target, err := GetToken(config)
	if err != nil {
		logger.Fatal("get-token-failed", err)
	}

	// Should be fixed once this gets merged: https://github.com/concourse/concourse/pull/4291
	event.RegisterEvent(event.InitializeGet{})
	event.RegisterEvent(event.InitializePut{})

	logger.Debug("filtering-builds")
	builds := utils.FilterBuilds(target.Team(), time.Unix(0, 0))

	for i, build := range builds {
		if cacher.IsProcessed(build.ID) {
			logger.Debug("build-already-processed", lager.Data{"build-id": build.ID})
			continue
		}

		logger.Debug("getting-metrics", lager.Data{"build": build, "current-index": i, "max-index": len(builds)})
		buildMetric, err := GetMetrics(target.Client(), build)
		if err != nil {
			logger.Fatal("get-metrics-failed", err)
		}

		utils.UglyPrint(buildMetric)

		cacher.MarkProcessed(build.ID)
	}
}
