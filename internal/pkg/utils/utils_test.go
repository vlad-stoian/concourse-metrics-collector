package utils_test

import (
	"github.com/concourse/concourse/atc"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vlad-stoian/concourse-metrics-collector/internal/pkg/utils"
)

func PutStep(name string) atc.Plan {
	return atc.Plan{
		ID:  atc.PlanID(name),
		Put: &atc.PutPlan{Name: name},
	}
}

func GetStep(name string) atc.Plan {
	return atc.Plan{
		ID:  atc.PlanID(name),
		Get: &atc.GetPlan{Name: name},
	}
}

func TaskStep(name string) atc.Plan {
	return atc.Plan{
		ID:   atc.PlanID(name),
		Task: &atc.TaskPlan{Name: name},
	}
}

func ZeroLevels() atc.Plan {
	return atc.Plan{
		ID: "level-zero",

		Aggregate: &atc.AggregatePlan{
			PutStep("level-zero-aggregate-put"),
			GetStep("level-zero-aggregate-get"),
			TaskStep("level-zero-aggregate-task"),
		},
		InParallel: &atc.InParallelPlan{
			Steps: []atc.Plan{
				PutStep("level-zero-in-parallel-put"),
				GetStep("level-zero-in-parallel-get"),
				TaskStep("level-zero-in-parallel-task"),
			},
		},
		Do: &atc.DoPlan{
			PutStep("level-zero-do-put"),
			GetStep("level-zero-do-get"),
			TaskStep("level-zero-do-task"),
		},
		Get:  nil,
		Put:  nil,
		Task: nil,

		OnAbort: &atc.OnAbortPlan{
			Step: TaskStep("level-zero-on-abort-step-task"),
			Next: TaskStep("level-zero-on-abort-next-task"),
		},
		OnError: &atc.OnErrorPlan{
			Step: TaskStep("level-zero-on-error-step-task"),
			Next: TaskStep("level-zero-on-error-next-task"),
		},
		Ensure: &atc.EnsurePlan{
			Step: TaskStep("level-zero-ensure-step-task"),
			Next: TaskStep("level-zero-ensure-next-task"),
		},
		OnSuccess: &atc.OnSuccessPlan{
			Step: TaskStep("level-zero-on-success-step-task"),
			Next: TaskStep("level-zero-on-success-next-task"),
		},
		OnFailure: &atc.OnFailurePlan{
			Step: TaskStep("level-zero-on-failure-step-task"),
			Next: TaskStep("level-zero-on-failure-next-task"),
		},
		Try: &atc.TryPlan{
			Step: PutStep("level-zero-try-put"),
		},
		Timeout: &atc.TimeoutPlan{
			Step: GetStep("level-zero-timeout-get"),
		},
		Retry: &atc.RetryPlan{
			PutStep("level-zero-retry-put"),
			GetStep("level-zero-retry-get"),
			TaskStep("level-zero-retry-task"),
		},
	}
}

var _ = Describe("Utils", func() {

	It("should collects all the ids recursively with CollectIds", func() {
		thePlan := ZeroLevels()

		collectedIDs := utils.CollectIDs(thePlan)

		Expect(collectedIDs).To(HaveKey("level-zero-aggregate-put"))
		Expect(collectedIDs).To(HaveKey("level-zero-aggregate-get"))
		Expect(collectedIDs).To(HaveKey("level-zero-aggregate-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-in-parallel-put"))
		Expect(collectedIDs).To(HaveKey("level-zero-in-parallel-get"))
		Expect(collectedIDs).To(HaveKey("level-zero-in-parallel-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-do-put"))
		Expect(collectedIDs).To(HaveKey("level-zero-do-get"))
		Expect(collectedIDs).To(HaveKey("level-zero-do-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-on-abort-step-task"))
		Expect(collectedIDs).To(HaveKey("level-zero-on-abort-next-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-on-error-step-task"))
		Expect(collectedIDs).To(HaveKey("level-zero-on-error-next-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-ensure-step-task"))
		Expect(collectedIDs).To(HaveKey("level-zero-ensure-next-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-on-success-step-task"))
		Expect(collectedIDs).To(HaveKey("level-zero-on-success-next-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-on-failure-step-task"))
		Expect(collectedIDs).To(HaveKey("level-zero-on-failure-next-task"))

		Expect(collectedIDs).To(HaveKey("level-zero-try-put"))

		Expect(collectedIDs).To(HaveKey("level-zero-timeout-get"))

		Expect(collectedIDs).To(HaveKey("level-zero-retry-put"))
		Expect(collectedIDs).To(HaveKey("level-zero-retry-get"))
		Expect(collectedIDs).To(HaveKey("level-zero-retry-task"))

	})

})
