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

func CollectIDs(plan atc.Plan, ids map[string]models.TaskMetric) {
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
		ids[planID] = models.TaskMetric{
			ID:   planID,
			Name: plan.Task.Name,
			Type: "task",
		}
	}

	if plan.Get != nil {
		ids[planID] = models.TaskMetric{
			ID:   planID,
			Name: plan.Get.Name,
			Type: "get",
		}
	}

	if plan.Put != nil {
		ids[planID] = models.TaskMetric{
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
