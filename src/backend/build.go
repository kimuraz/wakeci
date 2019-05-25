package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"

	bolt "github.com/etcd-io/bbolt"
)

// NumberOfConcurrentBuilds maximum amount of tasks that are being executed at the same time
const NumberOfConcurrentBuilds = 2

// BuildList contains all tasks that are being executed at the moment
var BuildList []*Build

// BuildQueue ...
var BuildQueue []*Build

// BuildStatus ...
type BuildStatus string

// BuildRunning ...
const BuildRunning = "running"

// BuildFailed ...
const BuildFailed = "failed"

// BuildFinished ...
const BuildFinished = "finished"

// BuildPending ...
const BuildPending = "pending"

// Build ...
type Build struct {
	ID        string // job.Name + Count
	Job       *Job
	Count     int
	Status    BuildStatus
	DoneTasks int // to report progress
	Logger    *log.Logger
}

// Start starts execution of tasks in job
func (b *Build) Start() {
	b.Logger.Println("Started...")
	b.Status = BuildRunning
	b.BroadcastUpdate()
	err := os.MkdirAll(WorkspaceDir+b.ID+"/", os.ModePerm)
	if err != nil {
		b.Logger.Println(err)
		b.Failed()
	}
	for _, task := range b.Job.Tasks {
		args := append([]string{"-c", task.Command})
		cmd := exec.Command("sh", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			b.Logger.Println(err)
			b.Failed()
			return
		}
		b.Logger.Println(string(out))
		b.DoneTasks++
		b.BroadcastUpdate()
	}
	b.Finished()
}

// Failed is called when job fails
func (b *Build) Failed() {
	b.Logger.Println("Failed.")
	b.Status = BuildFailed
	b.BroadcastUpdate()
	b.Cleanup()
}

// Finished is called when a job succeded
func (b *Build) Finished() {
	b.Logger.Println("Finished.")
	b.Status = BuildFinished
	b.BroadcastUpdate()
	b.Cleanup()
}

// Cleanup is called when a job finished or filed
func (b *Build) Cleanup() {
	for i, ex := range BuildList {
		if ex.ID == b.ID {
			BuildList = append(BuildList[:i], BuildList[i+1:]...)
			break
		}
	}
	TakeFromQueue()
}

// BroadcastUpdate ...
func (b *Build) BroadcastUpdate() {
	msg := MsgBuildUpdate{
		Type: "build:update",
		Data: &BuildUpdateData{
			ID:         b.ID,
			Count:      b.Count,
			Name:       b.Job.Name,
			Status:     b.Status,
			TotalTasks: len(b.Job.Tasks),
			DoneTasks:  b.DoneTasks,
		},
	}
	msgB, err := json.Marshal(msg)
	if err != nil {
		Logger.Println(err)
		return
	}
	BroadcastChannel <- msgB
}

// CreateBuild ..
func CreateBuild(job *Job) (*Build, error) {
	var id int
	err := DB.Update(func(tx *bolt.Tx) error {
		var err error
		b := tx.Bucket([]byte(JobsBucket))
		j := b.Bucket([]byte(job.Name))
		if j == nil {
			return fmt.Errorf("No job with name %s", job.Name)
		}
		idS := string(j.Get([]byte("count")))
		id, err = strconv.Atoi(idS)
		if err != nil {
			return err
		}
		id = id + 1
		err = j.Put([]byte("count"), []byte(strconv.Itoa(id)))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Broadcast job count update
	msg := MsgJobUpdate{
		Type: "job:update",
		Data: &JobsListData{
			Name:  job.Name,
			Count: id,
		},
	}
	msgB, err := json.Marshal(msg)
	if err != nil {
		Logger.Println(err)
	}
	BroadcastChannel <- msgB

	build := Build{
		Job:    job,
		Status: BuildPending,
		Count:  id,
		ID:     fmt.Sprintf("%s_%d", job.Name, id),
	}
	build.Logger = log.New(os.Stdout, build.ID, log.Lmicroseconds|log.Lshortfile)
	return &build, nil
}

// TakeFromQueue checks if it is possible to start executing new job from queue
// and executes it
func TakeFromQueue() {
	if len(BuildList) < NumberOfConcurrentBuilds && len(BuildQueue) > 0 {
		Logger.Printf("Taking job from queue %s\n", BuildQueue[0].ID)
		BuildList = append(BuildList, BuildQueue[0])
		go BuildQueue[0].Start()
		BuildQueue[0] = nil
		BuildQueue = BuildQueue[1:]
		TakeFromQueue()
	}
	Logger.Printf("Executing %d jobs, %d in queue\n", len(BuildList), len(BuildQueue))
}
