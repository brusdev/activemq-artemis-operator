package main

import (
	"flag"
	"fmt"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/apis/broker/v2alpha3"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/cr2jinja2/v2alpha3"
	"io/ioutil"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	//"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"bytes"
	//"os"
	//"gopkg.in/yaml.v2"
	//"encoding/json"
)

/*
Usage cr2jinja2 [options] broker_cr.yaml
options:
-e the environment variable
-o the output file
*/
func main() {

	envVar := flag.String("e", "", "env var")
	outputVar := flag.String("o", "default.yaml", "output file")

	flag.Parse()

	fmt.Println("-e:", *envVar)
	fmt.Println("-o", *outputVar)
	fmt.Println("tail: ", flag.Args())
	if len(flag.Args()) != 1 {
		fmt.Println("you need to pass in the cr file")
		return
	}
	crFile := flag.Args()[0]
	fmt.Println("cr file " + crFile)
	yamlFile, err := ioutil.ReadFile(crFile)

	if err != nil {
		fmt.Println("err", err)
		return
	}

	brokerCr := v2alpha3.ActiveMQArtemis{}

	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(yamlFile), 1024)
	err = decoder.Decode(&brokerCr)

	if err != nil {
		fmt.Printf("Error parsing YAML file: %s\n", err)
		return
	}

	result := cr2jinja2.MakeBrokerCfgOverrides(&brokerCr, envVar, outputVar)

	fmt.Println("result: \n" + result)

}
