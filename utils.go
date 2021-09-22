package main

import (
	"encoding/json"
	"log"
)

func marshalWrapper(v interface{}) []byte {
	r, err := json.Marshal(v)
	if err != nil{
		log.Fatal(err.Error())
	}
	return r
}

func printResults(){

}
