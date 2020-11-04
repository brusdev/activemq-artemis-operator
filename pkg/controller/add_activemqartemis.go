package controller

import (
	v2alpha4 "github.com/artemiscloud/activemq-artemis-operator/pkg/controller/broker/v2alpha4/activemqartemis"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, v2alpha4.Add)
}
