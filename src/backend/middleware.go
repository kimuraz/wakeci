package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	bolt "github.com/etcd-io/bbolt"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/crypto/bcrypt"
)

// HandlerLogger is a special type for loggers per request
type HandlerLogger string

// HL is a handle logger
const HL HandlerLogger = "logger"

// LogMi is a middleware that creates a new logger per request and logs total time that took to process a request
func LogMi(next httprouter.Handle) httprouter.Handle {
	return httprouter.Handle(func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		startTime := time.Now()

		// Take the context out from the request
		ctx := r.Context()

		logID := GenerateRandomString(5)

		// Get the settings
		handlerLogger := log.New(os.Stdout, "["+logID+"] ", log.Lmicroseconds|log.Lshortfile)

		// Get new context with key-value "settings"
		ctx = context.WithValue(ctx, HL, handlerLogger)

		// Get new http.Request with the new context
		r = r.WithContext(ctx)

		// Call actuall handler
		next(w, r, ps)

		defer func() {
			duration := time.Now().Sub(startTime)
			handlerLogger.Printf("%s %s [took %s]\n", r.Method, r.URL, duration)
		}()
	})
}

// CORSMi adds CORS headers
func CORSMi(next httprouter.Handle) httprouter.Handle {
	return httprouter.Handle(func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Call actuall handler
		next(w, r, ps)
		origin := "*"
		if *HostnameFlag != "" {
			origin = "https://" + *HostnameFlag
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Max-Age", "86400")
	})
}

// AuthMi adds CORS headers
func AuthMi(next httprouter.Handle) httprouter.Handle {
	return httprouter.Handle(func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Basic auth for API calls
		_, password, ok := r.BasicAuth()
		if ok {
			var hashedPassword []byte

			err := DB.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte(GlobalBucket))
				hashedPassword = b.Get([]byte("password"))
				return nil
			})

			err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(password))
			if err != nil {
				Logger.Println(err)
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r, ps)
			return
		}

		// Session auth for vue calls
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
		next(w, r, ps)
	})
}

// VueResourcesMi checks if path needs to be stripped out before serving the location
func VueResourcesMi(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First check if it is any of API or AUTH calls
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			w.WriteHeader(http.StatusNotFound)
			Logger.Printf("vue 404 %s\n", r.URL.Path)
			return
		}
		// Static file or root address
		if strings.Contains(r.URL.Path, ".") || r.URL.Path == "/" {
			Logger.Printf("vue GET %s\n", r.URL.Path)
			h.ServeHTTP(w, r)
			return
		}
		// Most likely this is request to one of the dynamic URLs used by frontend,
		// serve index.html (/) in this case
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = "/"
		Logger.Printf("vue %s --> %s\n", r.URL.Path, r2.URL.Path)
		h.ServeHTTP(w, r2)
	})
}
