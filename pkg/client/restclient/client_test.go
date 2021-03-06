/*
Copyright 2014 The Kubernetes Authors.

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

package restclient

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/apimachinery/registered"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/diff"
	utiltesting "k8s.io/kubernetes/pkg/util/testing"
)

type TestParam struct {
	actualError           error
	expectingError        bool
	actualCreated         bool
	expCreated            bool
	expStatus             *unversioned.Status
	testBody              bool
	testBodyErrorIsNotNil bool
}

func TestDoRequestSuccess(t *testing.T) {
	testServer, fakeHandler, status := testServerEnv(t, 200)
	defer testServer.Close()

	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, err := c.Get().Prefix("test").Do().Raw()

	testParam := TestParam{actualError: err, expectingError: false, expCreated: true,
		expStatus: status, testBody: true, testBodyErrorIsNotNil: false}
	validate(testParam, t, body, fakeHandler)
}

func TestDoRequestFailed(t *testing.T) {
	status := &unversioned.Status{
		Code:    http.StatusNotFound,
		Status:  unversioned.StatusFailure,
		Reason:  unversioned.StatusReasonNotFound,
		Message: " \"\" not found",
		Details: &unversioned.StatusDetails{},
	}
	expectedBody, _ := runtime.Encode(testapi.Default.Codec(), status)
	fakeHandler := utiltesting.FakeHandler{
		StatusCode:   404,
		ResponseBody: string(expectedBody),
		T:            t,
	}
	testServer := httptest.NewServer(&fakeHandler)
	defer testServer.Close()

	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = c.Get().Do().Error()
	if err == nil {
		t.Errorf("unexpected non-error")
	}
	ss, ok := err.(errors.APIStatus)
	if !ok {
		t.Errorf("unexpected error type %v", err)
	}
	actual := ss.Status()
	if !reflect.DeepEqual(status, &actual) {
		t.Errorf("Unexpected mis-match: %s", diff.ObjectReflectDiff(status, &actual))
	}
}

func TestDoRawRequestFailed(t *testing.T) {
	status := &unversioned.Status{
		Code:    http.StatusNotFound,
		Status:  unversioned.StatusFailure,
		Reason:  unversioned.StatusReasonNotFound,
		Message: "the server could not find the requested resource",
		Details: &unversioned.StatusDetails{
			Causes: []unversioned.StatusCause{
				{Type: unversioned.CauseTypeUnexpectedServerResponse, Message: "unknown"},
			},
		},
	}
	expectedBody, _ := runtime.Encode(testapi.Default.Codec(), status)
	fakeHandler := utiltesting.FakeHandler{
		StatusCode:   404,
		ResponseBody: string(expectedBody),
		T:            t,
	}
	testServer := httptest.NewServer(&fakeHandler)
	defer testServer.Close()

	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, err := c.Get().Do().Raw()

	if err == nil || body == nil {
		t.Errorf("unexpected non-error: %#v", body)
	}
	ss, ok := err.(errors.APIStatus)
	if !ok {
		t.Errorf("unexpected error type %v", err)
	}
	actual := ss.Status()
	if !reflect.DeepEqual(status, &actual) {
		t.Errorf("Unexpected mis-match: %s", diff.ObjectReflectDiff(status, &actual))
	}
}

func TestDoRequestCreated(t *testing.T) {
	testServer, fakeHandler, status := testServerEnv(t, 201)
	defer testServer.Close()

	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created := false
	body, err := c.Get().Prefix("test").Do().WasCreated(&created).Raw()

	testParam := TestParam{actualError: err, expectingError: false, expCreated: true,
		expStatus: status, testBody: false}
	validate(testParam, t, body, fakeHandler)
}

func TestDoRequestNotCreated(t *testing.T) {
	testServer, fakeHandler, expectedStatus := testServerEnv(t, 202)
	defer testServer.Close()
	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created := false
	body, err := c.Get().Prefix("test").Do().WasCreated(&created).Raw()
	testParam := TestParam{actualError: err, expectingError: false, expCreated: false,
		expStatus: expectedStatus, testBody: false}
	validate(testParam, t, body, fakeHandler)
}

func TestDoRequestAcceptedNoContentReturned(t *testing.T) {
	testServer, fakeHandler, _ := testServerEnv(t, 204)
	defer testServer.Close()

	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created := false
	body, err := c.Get().Prefix("test").Do().WasCreated(&created).Raw()
	testParam := TestParam{actualError: err, expectingError: false, expCreated: false,
		testBody: false}
	validate(testParam, t, body, fakeHandler)
}

func TestBadRequest(t *testing.T) {
	testServer, fakeHandler, _ := testServerEnv(t, 400)
	defer testServer.Close()
	c, err := restClient(testServer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created := false
	body, err := c.Get().Prefix("test").Do().WasCreated(&created).Raw()
	testParam := TestParam{actualError: err, expectingError: true, expCreated: false,
		testBody: true}
	validate(testParam, t, body, fakeHandler)
}

func validate(testParam TestParam, t *testing.T, body []byte, fakeHandler *utiltesting.FakeHandler) {
	if testParam.expectingError {
		if testParam.actualError == nil {
			t.Errorf("Expected error")
		}
	}
	if !testParam.expCreated {
		if testParam.actualCreated {
			t.Errorf("Expected object not to be created")
		}
	}
	statusOut, err := runtime.Decode(testapi.Default.Codec(), body)
	if testParam.testBody {
		if testParam.testBodyErrorIsNotNil {
			if err == nil {
				t.Errorf("Expected Error")
			}
		}
	}

	if testParam.expStatus != nil {
		if !reflect.DeepEqual(testParam.expStatus, statusOut) {
			t.Errorf("Unexpected mis-match. Expected %#v.  Saw %#v", testParam.expStatus, statusOut)
		}
	}
	fakeHandler.ValidateRequest(t, "/"+registered.GroupOrDie(api.GroupName).GroupVersion.String()+"/test", "GET", nil)

}

func TestHttpMethods(t *testing.T) {
	testServer, _, _ := testServerEnv(t, 200)
	defer testServer.Close()
	c, _ := restClient(testServer)

	request := c.Post()
	if request == nil {
		t.Errorf("Post : Object returned should not be nil")
	}

	request = c.Get()
	if request == nil {
		t.Errorf("Get: Object returned should not be nil")
	}

	request = c.Put()
	if request == nil {
		t.Errorf("Put : Object returned should not be nil")
	}

	request = c.Delete()
	if request == nil {
		t.Errorf("Delete : Object returned should not be nil")
	}

	request = c.Patch(api.JSONPatchType)
	if request == nil {
		t.Errorf("Patch : Object returned should not be nil")
	}
}

func TestCreateBackoffManager(t *testing.T) {

	theUrl, _ := url.Parse("http://localhost")

	// 1 second base backoff + duration of 2 seconds -> exponential backoff for requests.
	os.Setenv(envBackoffBase, "1")
	os.Setenv(envBackoffDuration, "2")
	backoff := readExpBackoffConfig()
	backoff.UpdateBackoff(theUrl, nil, 500)
	backoff.UpdateBackoff(theUrl, nil, 500)
	if backoff.CalculateBackoff(theUrl)/time.Second != 2 {
		t.Errorf("Backoff env not working.")
	}

	// 0 duration -> no backoff.
	os.Setenv(envBackoffBase, "1")
	os.Setenv(envBackoffDuration, "0")
	backoff.UpdateBackoff(theUrl, nil, 500)
	backoff.UpdateBackoff(theUrl, nil, 500)
	backoff = readExpBackoffConfig()
	if backoff.CalculateBackoff(theUrl)/time.Second != 0 {
		t.Errorf("Zero backoff duration, but backoff still occuring.")
	}

	// No env -> No backoff.
	os.Setenv(envBackoffBase, "")
	os.Setenv(envBackoffDuration, "")
	backoff = readExpBackoffConfig()
	backoff.UpdateBackoff(theUrl, nil, 500)
	backoff.UpdateBackoff(theUrl, nil, 500)
	if backoff.CalculateBackoff(theUrl)/time.Second != 0 {
		t.Errorf("Backoff should have been 0.")
	}

}

func testServerEnv(t *testing.T, statusCode int) (*httptest.Server, *utiltesting.FakeHandler, *unversioned.Status) {
	status := &unversioned.Status{Status: fmt.Sprintf("%s", unversioned.StatusSuccess)}
	expectedBody, _ := runtime.Encode(testapi.Default.Codec(), status)
	fakeHandler := utiltesting.FakeHandler{
		StatusCode:   statusCode,
		ResponseBody: string(expectedBody),
		T:            t,
	}
	testServer := httptest.NewServer(&fakeHandler)
	return testServer, &fakeHandler, status
}

func restClient(testServer *httptest.Server) (*RESTClient, error) {
	c, err := RESTClientFor(&Config{
		Host: testServer.URL,
		ContentConfig: ContentConfig{
			GroupVersion:         &registered.GroupOrDie(api.GroupName).GroupVersion,
			NegotiatedSerializer: testapi.Default.NegotiatedSerializer(),
		},
		Username: "user",
		Password: "pass",
	})
	return c, err
}
