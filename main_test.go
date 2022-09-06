package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestSawer(t *testing.T) {
	type Sawer_In struct {
		FromUsername        string `json:"-"`
		DestinationUsername string `json:"destination"`
		Amount              uint64 `json:"amount"`
		Message             string `json:"message"`
	}
	in := Sawer_In{
		DestinationUsername: `akfa`,
		Amount:              50000,
		Message:             `duar bebek`,
	}
	b := &bytes.Buffer{}
	json.NewEncoder(b).Encode(in)
	req, err := http.NewRequest(`POST`, `http://localhost:6090/sawer`, b)
	if err != nil {
		panic(err)
	}
	req.AddCookie(&http.Cookie{
		Name:  `username`,
		Value: `akbarfa`,
	})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	fmt.Println(res.StatusCode)
	by, _ := io.ReadAll(req.Body)
	fmt.Println(string(by))
}
