package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"

	bolt "github.com/etcd-io/bbolt"
	uuid "github.com/gofrs/uuid"
	"github.com/julienschmidt/httprouter"
)

// HandleRunJob adds job to queue
func HandleRunJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	jobFile := WorkingDir + ps.ByName("name") + ".yaml"
	job, err := ReadJob(jobFile)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	build, err := CreateBuild(job)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Update params from URL
	for idx := range build.Params {
		for pkey := range build.Params[idx] {
			value := r.URL.Query().Get(pkey)
			if value != "" {
				build.Params[idx][pkey] = value
				Logger.Printf("Updating key %s to %s", pkey, value)
			}
		}
	}

	// Create workspace
	err = os.MkdirAll(build.GetWorkspaceDir(), os.ModePerm)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	Logger.Printf("Workspace %s has been created\n", build.GetWorkspaceDir())

	// Create wakespace
	err = os.MkdirAll(build.GetWakespaceDir(), os.ModePerm)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	Logger.Printf("Wakespace %s has been created\n", build.GetWakespaceDir())

	// Copy job config
	input, err := ioutil.ReadFile(jobFile)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = ioutil.WriteFile(build.GetBuildConfigFilename(), input, 0644)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	Logger.Printf("Build config %s has been created\n", build.GetBuildConfigFilename())

	Q.Add(build)
	Q.Take()
	build.BroadcastUpdate()
	defer w.Write([]byte(strconv.Itoa(build.ID)))
}

// HandleGetBuild Returns information required to bootstrap build page
func HandleGetBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	idp := ps.ByName("id")
	buildID, err := strconv.Atoi(idp)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// Collect tasks info by reconstructing jon object
	buildConfigFilename := WorkingDir + "wakespace/" + strconv.Itoa(buildID) + "/build.yaml"
	if _, err := os.Stat(buildConfigFilename); os.IsNotExist(err) {
		Logger.Println(err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	job, err := ReadJob(buildConfigFilename)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Get build statusupdate
	var buildStatusData BuildUpdateData
	err = DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(HistoryBucket))
		ud := b.Get(Itob(buildID))
		if ud == nil {
			return fmt.Errorf("Not found")
		}
		return json.Unmarshal(ud, &buildStatusData)
	})
	if err != nil {
		Logger.Println(err)
		if err.Error() == "Not found" {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}
	payload := struct {
		Job          *Job             `json:"job"`
		StatusUpdate *BuildUpdateData `json:"status_update"`
	}{
		Job:          job,
		StatusUpdate: &buildStatusData,
	}

	payloadB, err := json.Marshal(payload)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(payloadB)
}

// HandleFeedView returns items in current feed - executed and queued jobs
func HandleFeedView(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	const pageSize = 10
	var payload []*BuildUpdateData
	err := DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(HistoryBucket))
		c := b.Cursor()
		count := 0
		for key, _ := c.Last(); key != nil; key, _ = c.Prev() {
			var msg BuildUpdateData
			err := json.Unmarshal(b.Get(key), &msg)
			if err != nil {
				Logger.Println(err)
			} else {
				switch msg.Status {
				case StatusPending, StatusRunning:
					if !Q.Verify(msg.ID) {
						msg.Status = StatusAborted
						updatedB, err := json.Marshal(msg)
						if err != nil {
							Logger.Println(err)
						}
						b.Put(Itob(msg.ID), updatedB)
					}
					break
				}
				payload = append(payload, &msg)
				count++
				if count >= pageSize {
					break
				}
			}
		}
		return nil
	})
	payloadB, err := json.Marshal(payload)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(payloadB)
}

// HandleJobsView returns all available jobs
func HandleJobsView(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var data []*JobsListData
	err := DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(JobsBucket))
		c := b.Cursor()
		for key, _ := c.First(); key != nil; key, _ = c.Next() {
			job := JobsListData{
				Name: string(key),
			}
			jb := b.Bucket(key)
			if jb != nil {
				params := jb.Get([]byte("defaultParams"))
				err := json.Unmarshal(params, &job.DefaultParams)
				if err != nil {
					return err
				}
			}
			data = append(data, &job)
		}
		return nil
	})
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	payloadB, err := json.Marshal(data)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(payloadB)
}

// HandleReloadTaskLog broadcasts all logs from a filesystem file
func HandleReloadTaskLog(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	buildID := ps.ByName("id")
	taskID := ps.ByName("taskID")
	// Verify ids
	_, err := strconv.Atoi(buildID)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	taskIDint, err := strconv.Atoi(taskID)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	path := WorkingDir + "wakespace/" + buildID + "/" + "task_" + taskID + ".log"
	// Verify that path exists
	_, err = os.Stat(path)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// Read file
	f, err := os.OpenFile(path, os.O_RDONLY, os.ModePerm)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	rd := bufio.NewReader(f)
	var counter int
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}

			Logger.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		msg := MsgBroadcast{
			Type: "build:log:" + buildID,
			Data: &CommandLogData{
				TaskID: taskIDint,
				ID:     counter,
				Data:   line,
			},
		}
		counter++
		BroadcastChannel <- &msg
	}
}

// HandleAbortBuild aborts build
func HandleAbortBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	buildID := ps.ByName("id")
	id, err := strconv.Atoi(buildID)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	err = Q.Abort(id)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
}

// HandleIsLoggedIn returns 200 if user is logged in
func HandleIsLoggedIn(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	sessionToken, err := r.Cookie("session")
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var expiresB []byte
	err = DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(SessionBucket))
		expiresB = b.Get([]byte(sessionToken.Value))
		return nil
	})
	if expiresB == nil {
		Logger.Printf("Session %s doesn't exist\n", sessionToken.Value)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var expires time.Time
	err = expires.GobDecode(expiresB)
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if expires.Before(time.Now()) {
		Logger.Println("Session expired")
		w.WriteHeader(http.StatusForbidden)
		return
	}
}

// HandleLogIn verifies password and logs the user in
func HandleLogIn(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// Create and store session token
	sessionToken, err := uuid.NewV4()
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	expires := time.Now().Add(time.Hour * 24 * 7)
	expiresB, err := expires.GobEncode()
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(SessionBucket))
		return b.Put([]byte(sessionToken.String()), expiresB)
	})
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:    "session",
		Value:   sessionToken.String(),
		Expires: expires,
		Path:    "/",
	})
	w.WriteHeader(http.StatusNoContent)
}

// HandleLogOut logs the user out
func HandleLogOut(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	sessionToken, err := r.Cookie("session")
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	err = DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(SessionBucket))
		return b.Delete([]byte(sessionToken.Value))
	})
	if err != nil {
		Logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:       "session",
		Value:      "delete",
		RawExpires: "Thu, Jan 01 1970 00:00:00 UTC",
		Path:       "/",
	})
	w.WriteHeader(http.StatusNoContent)
}
