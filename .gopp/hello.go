package main

import "fmt"

type Person struct {
	Name string
	Age  int
}

func (this *Person) Greet() string {

	return fmt.Sprintf("Hello %v, age %v", this.Name, this.Age)

}

type __gopp_Person interface {
	Greet() string
}

type GoppPerson = __gopp_Person

func main() {
	p := Person{Name: "Bob", Age: 42}

	fmt.Println(p.Greet())
}
