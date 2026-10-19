package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Note struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ready"))
	})

	mux.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Text string `json:"text"`
		}

		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil || input.Text == "" {
			http.Error(w, "invalid note", http.StatusBadRequest)
			return
		}

		note := Note{Text: input.Text}
		err := db.QueryRowContext(
			r.Context(),
			"INSERT INTO notes (text) VALUES ($1) RETURNING id",
			note.Text,
		).Scan(&note.ID)

		if err != nil {
			log.Printf("insert: %v", err)
			http.Error(w, "database error", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(note)
	})

	mux.HandleFunc("GET /notes", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(
			r.Context(),
			"SELECT id, text FROM notes ORDER BY id",
		)
		if err != nil {
			log.Printf("select: %v", err)
			http.Error(w, "database error", http.StatusServiceUnavailable)
			return
		}
		defer rows.Close()

		notes := []Note{}
		for rows.Next() {
			var n Note
			if err := rows.Scan(&n.ID, &n.Text); err != nil {
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}
			notes = append(notes, n)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(notes)
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
