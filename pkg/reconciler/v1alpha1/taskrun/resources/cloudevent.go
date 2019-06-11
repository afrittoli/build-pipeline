/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"
	"encoding/json"
  "errors"
  "fmt"

	"github.com/cloudevents/sdk-go/pkg/cloudevents"
	"github.com/cloudevents/sdk-go/pkg/cloudevents/types"
  "github.com/knative/eventing-sources/pkg/kncloudevents"
  "github.com/knative/pkg/apis"
  "go.uber.org/zap"

  "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
)

// TektonEventType holds the types of cloud events sent by Tekton
type TektonEventType string

const(
  // TektonTaskRunUnknown is sent for TaskRuns with "ConditionSucceeded" "Unknown"
  TektonTaskRunUnknown    TektonEventType = "TektonTaskRunUnknown"
  // TektonTaskRunSuccessful is sent for TaskRuns with "ConditionSucceeded" "True"
  TektonTaskRunSuccessful TektonEventType = "TektonTaskRunSuccessful"
  // TektonTaskRunFailed is sent for TaskRuns with "ConditionSucceeded" "False"
  TektonTaskRunFailed     TektonEventType = "TektonTaskRunFailed"
)

// SendCloudEvent sends a Cloud Event to the specified SinkURI
func SendCloudEvent(sinkURI, eventID, eventSourceURI string, data []byte, eventType TektonEventType, logger *zap.SugaredLogger) error {
  // Setup the cloudevent client
	cloudEventClient, err := kncloudevents.NewDefaultClient(sinkURI)
	if err != nil {
		logger.Errorf("Error creating the cloud-event client: %s", err)
    return err
	}

	event := cloudevents.Event{
		Context: cloudevents.EventContextV02{
			ID:         eventID,
			Type:       string(eventType),
			Source:     *types.ParseURLRef(eventSourceURI),
			Extensions: nil,
		}.AsV02(),
		Data: data,
	}
	_, err = cloudEventClient.Send(context.TODO(), event)
	if err != nil {
		logger.Errorf("Error sending the cloud-event: %s", err)
    return err
	}
  return nil
}

// SendTaskRunCloudEvent sends a cloud event for a TaskRun
func SendTaskRunCloudEvent(sinkURI string, taskRun *v1alpha1.TaskRun, logger *zap.SugaredLogger) error {
	// Check if the TaskRun is defined
	if taskRun == nil {
    return errors.New("Cannot send an event for an empty TaskRun")
  }
  eventID := taskRun.ObjectMeta.Name
  taskRunStatus := taskRun.Status.GetCondition(apis.ConditionSucceeded)
  var eventType TektonEventType
  if taskRunStatus.IsUnknown() {
    eventType = TektonTaskRunUnknown
  } else if taskRunStatus.IsFalse() {
    eventType = TektonTaskRunFailed
  } else if taskRunStatus.IsTrue() {
    eventType = TektonTaskRunSuccessful
  } else {
    return fmt.Errorf("Unknown condition for in TaskRun.Status %s", taskRunStatus)
  }
  eventSourceURI := taskRun.ObjectMeta.SelfLink
  data, _ := json.Marshal(taskRun)
  err := SendCloudEvent(sinkURI, eventID, eventSourceURI, data, eventType, logger)
  return err
}
