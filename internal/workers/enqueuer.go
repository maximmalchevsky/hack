package workers

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

type Enqueuer struct {
	client *asynq.Client
}

func NewEnqueuer(c *asynq.Client) *Enqueuer { return &Enqueuer{client: c} }

type SyncPayload struct {
	IntegrationID uuid.UUID `json:"integration_id"`
}

func (e *Enqueuer) EnqueueSyncIncremental(integrationID uuid.UUID) error {
	if e.client == nil {
		return nil
	}
	body, err := json.Marshal(SyncPayload{IntegrationID: integrationID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskSyncIncremental, body)
	_, err = e.client.Enqueue(task,
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(3),
		asynq.Timeout(5*time.Minute),
	)
	return err
}

func (e *Enqueuer) EnqueueSyncBackfill(integrationID uuid.UUID) error {
	if e.client == nil {
		return nil
	}
	body, err := json.Marshal(SyncPayload{IntegrationID: integrationID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskSyncBackfill, body)
	_, err = e.client.Enqueue(task,
		asynq.Queue(QueueCritical),
		asynq.MaxRetry(2),
		asynq.Timeout(10*time.Minute),
	)
	return err
}

type MetricsRecomputePayload struct {
	EmployeeID uuid.UUID `json:"employee_id"`
}

func (e *Enqueuer) EnqueueMetricsRecompute(employeeID uuid.UUID) error {
	if e.client == nil {
		return nil
	}
	body, err := json.Marshal(MetricsRecomputePayload{EmployeeID: employeeID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskMetricsRecompute, body)
	_, err = e.client.Enqueue(task,
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(2),
		asynq.Timeout(time.Minute),
	)
	return err
}

func (e *Enqueuer) EnqueueTasksReplanOne(employeeID uuid.UUID) error {
	if e.client == nil {
		return nil
	}
	body, err := json.Marshal(MetricsRecomputePayload{EmployeeID: employeeID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskTasksReplanOne, body)
	_, err = e.client.Enqueue(task,
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(2),
		asynq.Timeout(2*time.Minute),
	)
	return err
}

type AIRecommendPayload struct {
	EmployeeID uuid.UUID `json:"employee_id"`
}

func (e *Enqueuer) EnqueueAIRecommend(employeeID uuid.UUID) error {
	if e.client == nil {
		return nil
	}
	body, err := json.Marshal(AIRecommendPayload{EmployeeID: employeeID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskAIRecommend, body)
	_, err = e.client.Enqueue(task,
		asynq.Queue(QueueLow),
		asynq.MaxRetry(2),
		asynq.Timeout(2*time.Minute),
		asynq.ProcessIn(30*time.Second),
	)
	return err
}

func (e *Enqueuer) Close() error {
	if e.client == nil {
		return nil
	}
	return e.client.Close()
}

func MustNewEnqueuer(redisAddr, password string, db int) *Enqueuer {
	c := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: password,
		DB:       db,
	})
	if c == nil {
		panic(fmt.Errorf("asynq: failed to create client for %s", redisAddr))
	}
	return NewEnqueuer(c)
}
