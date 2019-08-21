package models

type BuildMetric struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`

	TeamName     string `json:"team_name"`
	PipelineName string `json:"pipeline_name"`
	JobName      string `json:"job_name"`

	Tasks []TaskMetric `json:"tasks"`
}

type TaskMetric struct {
	BuildId int    `json:"build_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`

	InitializeTime int64 `json:"initialize_time"`
	StartTime      int64 `json:"start_time"`
	FinishTime     int64 `json:"finish_time"`
}
