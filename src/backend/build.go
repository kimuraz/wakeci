package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	bolt "github.com/etcd-io/bbolt"
	"github.com/go-cmd/cmd"
	"github.com/gorilla/websocket"
)

// ItemStatus handles information about the item status (currently is used for
// both Builds and Tasks)
type ItemStatus string

// StatusRunning ...
const StatusRunning = "running"

// StatusFailed ...
const StatusFailed = "failed"

// StatusFinished ...
const StatusFinished = "finished"

// StatusPending ...
const StatusPending = "pending"

// StatusAborted ...
const StatusAborted = "aborted"

// Build ...
type Build struct {
	ID             int
	Job            *Job
	Status         ItemStatus
	Logger         *log.Logger
	Subscribers    []*websocket.Conn
	abortedChannel chan bool
	aborted        bool
	Params         []map[string]string
	Artifacts      []string
}

// Start starts execution of tasks in job
func (b *Build) Start() {
	b.Logger.Println("Started...")
	b.Status = StatusRunning
	b.BroadcastUpdate()
	for _, task := range b.Job.Tasks {
		task.Status = StatusRunning
		b.BroadcastUpdate()
		// Disable output buffering, enable streaming
		cmdOptions := cmd.Options{
			Buffered:  false,
			Streaming: true,
		}

		// Create Cmd with options
		taskCmd := cmd.NewCmdOptions(cmdOptions, "bash", "-c", task.Command)

		// Construct environment from params
		taskCmd.Env = os.Environ()
		taskCmd.Dir = b.GetWorkspaceDir()
		for idx := range b.Params {
			for pkey, pval := range b.Params[idx] {
				taskCmd.Env = append(taskCmd.Env, fmt.Sprintf("%s=%s", pkey, pval))
			}
		}

		fwChannel := make(chan bool)

		// Print STDOUT and STDERR lines streaming from CmdLogger
		go func() {
			file, err := os.Create(b.GetWakespaceDir() + fmt.Sprintf("task_%d.log", task.ID))
			bw := bufio.NewWriter(file)
			defer func() {
				bw.Flush()
				file.Close()
			}()
			if err != nil {
				// Allow command to start
				time.Sleep(10 * time.Millisecond)
				b.Logger.Println(err)
				taskCmd.Stop()
				return
			}

			// Add executed command to logs
			_, err = bw.WriteString(task.Command + "\n")
			if err != nil {
				b.Logger.Println(err)
			}
			b.PublishCommandLogs(task.ID, 0, task.Command)

			x := 1
			for {
				select {
				case line := <-taskCmd.Stdout:
					_, err := bw.WriteString(line + "\n")
					if err != nil {
						b.Logger.Println(err)
					}
					b.PublishCommandLogs(task.ID, x, line)
					x++
				case line := <-taskCmd.Stderr:
					_, err := bw.WriteString(line + "\n")
					if err != nil {
						b.Logger.Println(err)
					}
					b.PublishCommandLogs(task.ID, x, line)
					x++
				case <-fwChannel:
					return
				case toAbort := <-b.abortedChannel:
					b.Logger.Println("Aborting via abortedChannel")
					if toAbort {
						taskCmd.Stop()
						b.aborted = true
					}
				}
			}
		}()

		// Run and wait for Cmd to return, discard Status
		status := <-taskCmd.Start()

		// Cmd has finished but wait for goroutine to print all lines
		for len(taskCmd.Stdout) > 0 || len(taskCmd.Stderr) > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		// Signal to flush the file
		fwChannel <- true

		// Abort message was recieved via channel
		if b.aborted {
			b.Abort()
			return
		}

		if status.Exit != 0 {
			task.Status = StatusFailed
			b.Failed()
			return
		}
		task.Status = StatusFinished
		b.BroadcastUpdate()
	}
	b.Finished()
}

// Failed is called when job fails
func (b *Build) Failed() {
	b.Logger.Println("Failed.")
	b.Status = StatusFailed
	b.BroadcastUpdate()
	b.Cleanup()
}

// Finished is called when a job succeded
func (b *Build) Finished() {
	b.Logger.Println("Finished.")
	b.Status = StatusFinished
	b.CollectArtifacts()
	b.BroadcastUpdate()
	b.Cleanup()
}

// Cleanup is called when a job finished or failed
func (b *Build) Cleanup() {
	Q.Remove(b.ID)
	Q.Take()
}

// CollectArtifacts copies artifacts from workspace to wakespace
func (b *Build) CollectArtifacts() {
	for _, artPattern := range b.Job.Artifacts {
		pattern := b.GetWorkspaceDir() + artPattern
		files, err := filepath.Glob(pattern)
		if err != nil {
			b.Logger.Println(err)
			continue
		}
		// Create artifacts directory first
		err = os.MkdirAll(b.GetWakespaceDir()+"artifacts/", os.ModePerm)
		if err != nil {
			b.Logger.Println(err)
			return
		}
		for _, f := range files {
			b.Logger.Printf("Copying artifact %s...\n", f)
			c := cmd.NewCmd("cp", f, b.GetWakespaceDir()+"artifacts/")
			s := <-c.Start()
			if s.Exit != 0 {
				b.Logger.Printf("Exit code: %d\n", s.Exit)
			} else {
				_, afile := filepath.Split(f)
				b.Artifacts = append(b.Artifacts, afile)
			}
		}
	}
}

// Abort task execution
func (b *Build) Abort() {
	b.Logger.Println("Aborted.")
	b.Status = StatusAborted
	b.BroadcastUpdate()
	b.Cleanup()
}

// BroadcastUpdate sends update to all subscribed clients. Contains general
// information about the build
func (b *Build) BroadcastUpdate() {
	data := b.GenerateBuildUpdateData()
	msg := MsgBroadcast{
		Type: "build:update:" + strconv.Itoa(b.ID),
		Data: data,
	}
	BroadcastChannel <- &msg

	err := DB.Update(func(tx *bolt.Tx) error {
		var err error
		hb := tx.Bucket([]byte(HistoryBucket))
		dataB, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return hb.Put(Itob(data.ID), dataB)
	})
	if err != nil {
		b.Logger.Println(err)
	}
}

// GenerateBuildUpdateData generates BuildUpdateData
func (b *Build) GenerateBuildUpdateData() *BuildUpdateData {
	return &BuildUpdateData{
		ID:        b.ID,
		Name:      b.Job.Name,
		Status:    b.Status,
		Tasks:     b.GetTasksStatus(),
		Params:    b.Params,
		Artifacts: b.Artifacts,
	}
}

// PublishCommandLogs sends log update to all subscribed users
func (b *Build) PublishCommandLogs(taskID int, id int, data string) {
	msg := MsgBroadcast{
		Type: "build:log:" + strconv.Itoa(b.ID),
		Data: &CommandLogData{
			TaskID: taskID,
			ID:     id,
			Data:   data,
		},
	}
	BroadcastChannel <- &msg
}

// GetWorkspaceDir returns path to the workspace, where all user created files
// are stored
func (b *Build) GetWorkspaceDir() string {
	return *WorkingDirFlag + "workspace/" + strconv.Itoa(b.ID) + "/"
}

// GetWakespaceDir returns path to the data dir - there all build+wake related data is
// stored
func (b *Build) GetWakespaceDir() string {
	return *WorkingDirFlag + "wakespace/" + strconv.Itoa(b.ID) + "/"
}

// GetBuildConfigFilename returns build config filename (copy of the original job file)
func (b *Build) GetBuildConfigFilename() string {
	return b.GetWakespaceDir() + "build.yaml"
}

// GetTasksStatus list of tasks with their status
func (b *Build) GetTasksStatus() []*TaskStatus {
	var info []*TaskStatus
	for _, t := range b.Job.Tasks {
		info = append(info, &TaskStatus{
			ID:     t.ID,
			Status: t.Status,
		})
	}
	return info
}

// CreateBuild creates Build instance and all necessary files and folders in wakespace
func CreateBuild(job *Job, jobPath string) (*Build, error) {
	var counti int
	err := DB.Update(func(tx *bolt.Tx) error {
		var err error
		gb := tx.Bucket([]byte(GlobalBucket))
		count := gb.Get([]byte("count"))
		if count == nil {
			counti = 1
		} else {
			counti, err = ByteToInt(count)
			if err != nil {
				return err
			}
			counti++
		}
		gb.Put([]byte("count"), []byte(strconv.Itoa(counti)))
		return nil
	})
	if err != nil {
		return nil, err
	}

	build := Build{
		Job:            job,
		Status:         StatusPending,
		ID:             counti,
		abortedChannel: make(chan bool),
		Params:         job.DefaultParams,
	}
	build.Logger = log.New(os.Stdout, fmt.Sprintf("[build #%d] ", build.ID), log.Lmicroseconds|log.Lshortfile)

	// Create workspace
	err = os.MkdirAll(build.GetWorkspaceDir(), os.ModePerm)
	if err != nil {
		build.Logger.Println(err)
		return nil, err
	}
	build.Logger.Printf("Workspace %s has been created\n", build.GetWorkspaceDir())

	// Create wakespace
	err = os.MkdirAll(build.GetWakespaceDir(), os.ModePerm)
	if err != nil {
		build.Logger.Println(err)
		return nil, err
	}
	build.Logger.Printf("Wakespace %s has been created\n", build.GetWakespaceDir())

	// Copy job config
	input, err := ioutil.ReadFile(jobPath)
	if err != nil {
		build.Logger.Println(err)
		return nil, err
	}

	err = ioutil.WriteFile(build.GetBuildConfigFilename(), input, 0644)
	if err != nil {
		build.Logger.Println(err)
		return nil, err
	}
	build.Logger.Printf("Build config %s has been created\n", build.GetBuildConfigFilename())

	return &build, nil
}
