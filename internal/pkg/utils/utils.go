package utils

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/vlad-stoian/concourse-metrics-collector/internal/pkg/models"

	"github.com/concourse/concourse/go-concourse/concourse"

	"github.com/concourse/concourse/atc"
	"github.com/pkg/errors"
)

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

func CollectIDs(plan atc.Plan) map[string]models.TaskMetric {
	plans := []atc.Plan{
		plan,
	}

	taskMetrics := map[string]models.TaskMetric{}

	for len(plans) > 0 {
		currentPlan := plans[0]
		plans = plans[1:] //pop

		currentPlanID := string(currentPlan.ID)

		if currentPlan.Task != nil {
			taskMetrics[currentPlanID] = models.TaskMetric{
				ID:   currentPlanID,
				Name: currentPlan.Task.Name,
				Type: "task",
			}
		}

		if currentPlan.Get != nil {
			taskMetrics[currentPlanID] = models.TaskMetric{
				ID:   currentPlanID,
				Name: currentPlan.Get.Name,
				Type: "get",
			}
		}

		if currentPlan.Put != nil {
			taskMetrics[currentPlanID] = models.TaskMetric{
				ID:   currentPlanID,
				Name: currentPlan.Put.Name,
				Type: "put",
			}
		}

		if currentPlan.Aggregate != nil {
			plans = append(plans, *currentPlan.Aggregate...)
		}

		if currentPlan.InParallel != nil {
			plans = append(plans, currentPlan.InParallel.Steps...)
		}

		if currentPlan.Do != nil {
			plans = append(plans, *currentPlan.Do...)
		}

		if currentPlan.OnAbort != nil {
			plans = append(plans, currentPlan.OnAbort.Step, currentPlan.OnAbort.Next)
		}

		if currentPlan.OnError != nil {
			plans = append(plans, currentPlan.OnError.Step, currentPlan.OnError.Next)
		}

		if currentPlan.Ensure != nil {
			plans = append(plans, currentPlan.Ensure.Step, currentPlan.Ensure.Next)
		}

		if currentPlan.OnSuccess != nil {
			plans = append(plans, currentPlan.OnSuccess.Step, currentPlan.OnSuccess.Next)
		}

		if currentPlan.OnFailure != nil {
			plans = append(plans, currentPlan.OnFailure.Step, currentPlan.OnFailure.Next)
		}

		if currentPlan.Try != nil {
			plans = append(plans, currentPlan.Try.Step)
		}

		if currentPlan.Timeout != nil {
			plans = append(plans, currentPlan.Timeout.Step)
		}

		if currentPlan.Retry != nil {
			plans = append(plans, *currentPlan.Retry...)
		}
	}

	return taskMetrics
}

func PrettyPrint(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func UglyPrint(v interface{}) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
