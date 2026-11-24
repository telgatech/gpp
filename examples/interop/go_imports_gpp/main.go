package main

import (
	"fmt"

	"example.com/gpp-go-imports-gpp/greeting"
)

func main() {
	fmt.Println(greeting.Hello("Grace"))

	// Demonstrate how Go handles errors returned from GPP functions
	fmt.Println(greeting.Hello(""))

	if person, err := greeting.New("Grace"); err != nil {
		fmt.Println("Go handled returned error:", err)
	} else {
		fmt.Println("Go received GPP class:", person.Name, ";", person.Message())
	}

	if _, err := greeting.New(""); err != nil {
		fmt.Println("Go handled returned error:", err)
	}
}
