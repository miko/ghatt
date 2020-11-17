package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"os"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/PaesslerAG/jsonpath"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/itchyny/gojq"
	"github.com/joho/godotenv"
	"github.com/nsf/jsondiff"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tidwall/pretty"
)

var (
	opt = godog.Options{
		Output: colors.Colored(os.Stdout),
		Format: "progress", // or "pretty"
	}
	cookieJar     *cookiejar.Jar
	defaultMemory map[string]string
	seeded        string = "HTTP_ENDPOINT,GRAPHQL_ENDPOINT,RESET_ENDPOINT,RESET_METHOD,RESET_BODY"
	funcMap       template.FuncMap
  COMMIT string = 'commit'
  TAG string = 'tag'
)

type apiFeature struct {
	URL         string
	lastCode    int
	lastStatus  string
	lastBody    []byte
	lastErrors  []byte
	lastHeaders map[string]string
	memory      map[string]interface{}
	variables   map[string]interface{}
	headers     map[string]string
}

func (a *apiFeature) resetDatabase(*godog.Scenario) bool {
	if endpoint, ok := a.memory["RESET_ENDPOINT"]; ok {
		if endpoint != "" {
			url := endpoint.(string)
			log.Trace().Str("url", url).Msg("Reset DB")
			method := "GET"
			if m, ok := a.memory["RESET_METHOD"]; ok {
				method = m.(string)
			}
			var err error

			if body, ok := a.memory["RESET_BODY"]; ok {
				err = a.sendrequestTo(method, url, body.(string))
			} else {
				err = a.sendrequestTo(method, url, "")
			}
			if err != nil {
				fmt.Errorf("GOT ERROR: %s\n", err)
			}
			if a.lastCode != 200 {
				fmt.Errorf("AFTER DB RESET: code=%d status=%s body=%s\n", a.lastCode, a.lastStatus, a.lastBody)
			}
			return true
		} else {
			log.Trace().Msg("Skipping reset - empty RESET_ENDPOINT defined")
			return false
		}
	} else {
		log.Trace().Msg("Skipping reset - no RESET_ENDPOINT defined")
		return false
	}
}

func (a *apiFeature) resetResponse(sc *godog.Scenario) {
	log.Trace().Msg("Reset reponse")
	a.memory = map[string]interface{}{}
	for k, v := range defaultMemory {
		a.memory[k] = v
	}
	if a.resetDatabase(sc) {
		a.memory = map[string]interface{}{}
		for k, v := range defaultMemory {
			a.memory[k] = v
		}
	}
	a.lastBody = []byte("")
	a.lastCode = 0
	a.lastStatus = ""
	a.lastHeaders = map[string]string{}
	a.lastErrors = []byte("")
	a.variables = map[string]interface{}{}
	a.headers = map[string]string{}
}

func (a *apiFeature) getParsed(source string) string {
	tmpl, err := template.New("tpl").Funcs(funcMap).Parse(source)
	if err != nil {
		return ""
	}
	var tpl bytes.Buffer
	if err := tmpl.Execute(&tpl, a.memory); err != nil {
		return ""
	}
	log.Trace().Str("in", source).Str("out", tpl.String()).Msg("getParsed")
	return tpl.String()
}

func (a *apiFeature) iSendrequestTo(method, endpoint string) (err error) {
	return a.sendrequestTo(method, endpoint, "")
}
func (a *apiFeature) iSendrequestToWithData(method, path string, body *godog.DocString) (err error) {
	return a.sendrequestTo(method, path, body.Content)
}
func (a *apiFeature) sendrequestTo(method, path string, body string) (err error) {
	var url string
	path = a.getParsed(path)
	if path[0:4] == "http" {
		url = path
	} else {
		if _, ok := a.memory["HTTP_ENDPOINT"]; ok == false {
			return fmt.Errorf("No http endpoint defined. Please set HTTP_ENDPOINT env variable/memory.")
		}
		endpoint := a.memory["HTTP_ENDPOINT"].(string)
		if endpoint == "" {
			return fmt.Errorf("No http endpoint defined. Please set HTTP_ENDPOINT env variable/memory.")
		}
		url = endpoint + path
	}

	// handle panic
	defer func() {
		switch t := recover().(type) {
		case string:
			err = fmt.Errorf(t)
		case error:
			err = t
		}
	}()
	body = a.getParsed(body)
	log.Trace().Str("method", method).Str("url", url).Msg(body)
	req, err2 := http.NewRequest(method, url, strings.NewReader(body))
	if err2 != nil {
		return err2
	}
	for k, v := range a.headers {
		req.Header.Add(k, v)
		log.Trace().Str("key", k).Str("value", v).Msg("Add HTTP header")
	}
	client := http.DefaultClient
	client.Jar = cookieJar
	resp, err2 := client.Do(req)
	if err2 != nil {
		return err2
	}

	respBody, err2 := ioutil.ReadAll(resp.Body)
	if err2 != nil {
		return err2
	}
	defer resp.Body.Close()
	a.lastBody = respBody
	a.lastCode = resp.StatusCode
	a.lastStatus = resp.Status
	a.lastHeaders = map[string]string{}
	a.lastErrors = []byte("")
	for k, v := range resp.Header {
		log.Trace().Str("k", k).Str("v", v[0]).Msg("HDR IN")
		a.lastHeaders[k] = v[0]
	}
	log.Trace().Str("status", resp.Status).Int("code", resp.StatusCode).Msg(string(a.lastBody))
	return
}

func (a *apiFeature) theResponseCodeShouldBe(code int) error {
	if code != a.lastCode {
		return fmt.Errorf("expected response code to be: %d, but actual is: %d", code, a.lastCode)
	}
	return nil
}
func (a *apiFeature) theResponseHeaderShouldMatch(key, value string) error {
	key = strings.ToLower(key)
	log.Trace().Msgf("HEADERS: %#v", a.lastHeaders)
	for k, v := range a.lastHeaders {
		log.Trace().Str("k", k).Str("v", v).Str("key", key).Msg("Comparing")
		if strings.ToLower(k) == key {
			if v == value {
				return nil
			} else {
				return fmt.Errorf("expected header %s to be: %s, but actual is: %s", key, value, v)
			}
		}
	}
	return fmt.Errorf("expected header %s to be: %s, but found no such header", key, value)
}

func (a *apiFeature) theResponseShouldBe(body *godog.DocString) error {
	body.Content = a.getParsed(body.Content)

	if body.Content != string(a.lastBody) {
		return fmt.Errorf("expected response body to be: %s, but actual is: %s", body.Content, a.lastBody)
	}
	return nil
}

func (a *apiFeature) theResponseShouldMatchJSON(body *godog.DocString) (err error) {
	var expected, actual interface{}

	body.Content = a.getParsed(body.Content)
	// re-encode expected response
	if err = json.Unmarshal([]byte(body.Content), &expected); err != nil {
		return
	}

	// re-encode actual response too
	if err = json.Unmarshal(a.lastBody, &actual); err != nil {
		return
	}

	// the matching may be adapted per different requirements.
	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("expected JSON does not match actual, %v vs. %v expected=%s actual=%s", expected, actual, body.Content, a.lastBody)
	}
	return nil
}

func (a *apiFeature) theResponseErrorsShouldMatchJSON(body *godog.DocString) (err error) {
	var expected, actual interface{}

	body.Content = a.getParsed(body.Content)
	// re-encode expected response
	if err = json.Unmarshal([]byte(body.Content), &expected); err != nil {
		return
	}

	if err = json.Unmarshal(a.lastErrors, &actual); err != nil {
		return
	}

	// the matching may be adapted per different requirements.
	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("expected JSON does not match actual, %v vs. %v expected=%s actual=%s", expected, actual, body.Content, a.lastErrors)
	}
	return nil
}

func (a *apiFeature) theResponseShouldMatchSubsetOfJSON(body *godog.DocString) (err error) {
	opts := jsondiff.DefaultConsoleOptions()
	body.Content = a.getParsed(body.Content)
	d, s := jsondiff.Compare(a.lastBody, []byte(body.Content), &opts)
	switch d {
	case jsondiff.FullMatch:
		return nil
		break
	case jsondiff.SupersetMatch:
		return nil
		break
	case jsondiff.NoMatch:
		return fmt.Errorf("No match for s= %s a=[%s] b=[%s]", s, a.lastBody, body.Content)
		break
	default:
		return fmt.Errorf("Unsupported match type, %d  for s= %s a=[%s] b=[%s]", d, s, a.lastBody, body.Content)
	}
	return nil
}

func (a *apiFeature) theResponseJsonpathShouldMatch(path, value string) (err error) {
	var v interface{}
	path = a.getParsed(path)
	value = a.getParsed(value)
	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	res, err := jsonpath.Get(path, v)
	if err != nil {
		return err
	}
	if res != value {
		return fmt.Errorf("No match for value, expected=[%s] got=[%s] for path=[%s]", value, res, path)
	}
	return nil
}

func (a *apiFeature) theResponseJsonpathShouldMatchNumber(path, value string) (err error) {
	var v interface{}
	path = a.getParsed(path)
	value = a.getParsed(value)
	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	res, err := jsonpath.Get(path, v)
	if err != nil {
		return err
	}
	res = fmt.Sprintf("%d", int32(res.(float64)))
	if res != value {
		return fmt.Errorf("No match for value, expected=[%s] got=[%s] for path=[%s]", value, res, path)
	}
	return nil
}

func (a *apiFeature) theResponseJsonpathShouldMatchFloat(path, value string) (err error) {
	var v interface{}
	path = a.getParsed(path)
	value = a.getParsed(value)
	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	res, err := jsonpath.Get(path, v)
	if err != nil {
		return err
	}
	res = fmt.Sprintf("%f", res.(float64))
	if res != value {
		return fmt.Errorf("No match for value, expected=[%s] got=[%s] for path=[%s]", value, res, path)
	}
	return nil
}

func (a *apiFeature) theResponseJsonpathShouldMatchSubsetOfJson(path string, body *godog.DocString) error {
	var v interface{}
	path = a.getParsed(path)
	body.Content = a.getParsed(body.Content)
	err := json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	res, err := jsonpath.Get(path, v)
	if err != nil {
		return err
	}

	opts := jsondiff.DefaultConsoleOptions()
	ress, err := json.Marshal(res)
	if err != nil {
		return err
	}
	d, s := jsondiff.Compare(ress, []byte(body.Content), &opts)
	switch d {
	case jsondiff.FullMatch:
		return nil
		break
	case jsondiff.SupersetMatch:
		return nil
		break
	case jsondiff.NoMatch:
		return fmt.Errorf("No match for s= %s path=[%s] a=[%s] b=[%s]", s, path, ress, body.Content)
		break
	default:
		return fmt.Errorf("Unsupported match type, %d  for s= %s path=[%s] a=[%s] b=[%s]", d, s, path, ress, body.Content)
	}
	return nil
}

func (a *apiFeature) iRememberJsonpathAs(path, key string) error {
	var v interface{}
	err := json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	res, err := jsonpath.Get(path, v)
	if err != nil {
		return err
	}
	a.memory[key] = res
	log.Trace().Str("key", key).Str("val", res.(string)).Msg("Remembered")
	return nil
}

func (a *apiFeature) iRememberJqAs(path, key string) error {
	var v interface{}
	err := json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}

	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}
	var actual string
	iter := code.Run(v)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if v == nil {
			return err
		}
		actual = v.(string)
	}
	a.memory[key] = actual
	return nil
}

func (a *apiFeature) theResponseJsonpathShouldMatchJson(path string, body *godog.DocString) error {
	var v interface{}
	var expected, actual interface{}

	// re-encode expected response
	if err := json.Unmarshal([]byte(body.Content), &expected); err != nil {
		return err
	}

	err := json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	actual, err = jsonpath.Get(path, v)
	if err != nil {
		return err
	}

	// the matching may be adapted per different requirements.
	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("expected JSON does not match actual, %v vs. %v", expected, actual)
	}
	return nil
}
func (a *apiFeature) theResponseJqShouldMatchJson(path string, body *godog.DocString) (err error) {
	var v interface{}
	var expected, actual interface{}

	// re-encode expected response
	if err := json.Unmarshal([]byte(body.Content), &expected); err != nil {
		return err
	}

	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}

	iter := code.Run(v)
	var res struct {
		Error   string `json:"error,omitempty"`
		Message string `json:"message,omitempty"`
		Status  bool   `json:"status,omitempty"`
	}
	err = json.Unmarshal([]byte(a.lastBody), &res)
	if err == nil {
		if res.Status == false {
			return fmt.Errorf("%s: %s", res.Message, res.Error)
		}
	} else {
		return err
	}
	if iter == nil {
		return fmt.Errorf("Problem with response, expected:\n%s", body.Content)
	}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		actual = v
	}
	// the matching may be adapted per different requirements.
	if !reflect.DeepEqual(expected, actual) {
		ae, _ := json.Marshal(actual)
		return fmt.Errorf("expected JSON does not match actual, expected:\n%s\nactual:\n%s", body.Content, string(ae))
	}

	return nil
}

func (a *apiFeature) theResponseErrorsJqShouldMatchJson(path string, body *godog.DocString) (err error) {
	var v interface{}
	var expected, actual interface{}

	// re-encode expected response
	if err := json.Unmarshal([]byte(body.Content), &expected); err != nil {
		log.Error().Err(err).Msg("EEEERRRR22222")
		return err
	}
	if string(a.lastErrors) == "" {
		a.lastErrors = []byte(`[]`)
	}
	err = json.Unmarshal(a.lastErrors, &v)
	if err != nil {
		log.Error().Err(err).Msg("EEEERRRR22223")
		return err
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}

	iter := code.Run(v)
	var res []struct {
		Error   string `json:"error,omitempty"`
		Message string `json:"message,omitempty"`
		Status  bool   `json:"status,omitempty"`
	}
	err = json.Unmarshal([]byte(a.lastErrors), &res)
	if err != nil {
		return err
	}
	if iter == nil {
		return fmt.Errorf("Problem with response, expected:\n%s", body.Content)
	}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		actual = v
	}
	// the matching may be adapted per different requirements.
	if !reflect.DeepEqual(expected, actual) {
		ae, _ := json.Marshal(actual)
		return fmt.Errorf("expected JSON does not match actual, expected:\n%s\nactual:\n%s", body.Content, string(ae))
	}

	return nil
}

func (a *apiFeature) theResponseJqShouldMatchSubsetOfJson(path string, body *godog.DocString) (err error) {
	var v interface{}
	var expected interface{}
	var actual string

	// re-encode expected response
	if err := json.Unmarshal([]byte(body.Content), &expected); err != nil {
		return err
	}

	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	if v.(map[string]interface{})["error"] != nil {
		return fmt.Errorf("Bad query - got error: %s", v.(map[string]interface{})["error"].(string))
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}

	iter := code.Run(v)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		act, err := json.Marshal(v)
		if err != nil {
			return err
		}
		actual = string(act)
	}

	opts := jsondiff.DefaultConsoleOptions()
	d, s := jsondiff.Compare([]byte(actual), []byte(body.Content), &opts)
	switch d {
	case jsondiff.FullMatch:
		return nil
		break
	case jsondiff.SupersetMatch:
		return nil
		break
	case jsondiff.NoMatch:
		return fmt.Errorf("No match for s=[%s] got=[%s] expected=[%s] result=[%s]", s, a.lastBody, body.Content, actual)
		break
	default:
		return fmt.Errorf("Unsupported match type, %d  for s=[%s] got=[%s] expected=[%s] result=[%s]", d, s, a.lastBody, body.Content, actual)
	}
	return nil

}

func (a *apiFeature) theResponseJqShouldMatch(path, value string) (err error) {
	var v interface{}
	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}
	var actual string
	iter := code.Run(v)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err != nil {
			return err
		}
		if v == nil {
			return fmt.Errorf("No match for value, expected=[%s] got=[%s] for path=[%s]", value, actual, path)
		}
		actual = v.(string)
	}
	if actual != value {
		return fmt.Errorf("No match for value, expected=[%s] got=[%s] for path=[%s]", value, actual, path)
	}
	return nil
}

func (a *apiFeature) theResponseJqShouldMatchNumber(path string, value int) (err error) {
	var v interface{}
	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}
	var actual int
	iter := code.Run(v)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err != nil {
			return err
		}
		switch v.(type) {
		case int:
			actual = v.(int)
			break
		case float64:
			actual = int(v.(float64))
			break
		default:
			return errors.New("Cannot parse value as number")
		}
	}
	if actual != value {
		return fmt.Errorf("No match for value, expected=[%d] got=[%d] for path=[%s]", value, actual, path)
	}
	return nil
}

func (a *apiFeature) theResponseJqShouldMatchFloat(path string, value float64) (err error) {
	var v interface{}
	err = json.Unmarshal(a.lastBody, &v)
	if err != nil {
		return err
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}
	log.Trace().Str("q", query.String()).Msgf("float: %#v", code)
	var actual float64
	iter := code.Run(v)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err != nil {
			return err
		}
		actual = v.(float64)
	}
	if actual != value {
		return fmt.Errorf("No match for value, expected=[%f] got=[%f] for path=[%s]", value, actual, path)
	}
	return nil
}

/*
func (a *apiFeature) iExecuteQueryToWithVariables(path string, body *godog.DocString) error {
	var v interface{}
	err := json.Unmarshal([]byte(body.Content), &v)
	if err != nil {
		return err
	}
	c := struct {
		Query     string      `json:"query"`
		Variables interface{} `json:"variables"`
	}{Query: a.query, Variables: v}
	content, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return a.sendrequestTo("POST", path, string(content))
}
*/
func (a *apiFeature) iRememberAs(key, value string) error {
	a.memory[key] = a.getParsed(value)
	return nil
}
func (a *apiFeature) iRememberAsBody(key string, value *godog.DocString) error {
	a.memory[key] = a.getParsed(value.GetContent())
	return nil
}
func (a *apiFeature) iSetVariableAs(key, value string) error {
	a.variables[key] = a.getParsed(value)
	return nil
}
func (a *apiFeature) iSetVariableAsStringList(key, value string) error {
	a.variables[key] = strings.Split(value, ",")
	return nil
}
func (a *apiFeature) iSetVariableAsNumber(key string, value int) error {
	a.variables[key] = value
	return nil
}
func (a *apiFeature) iSetVariableAsFloat(key string, value float64) error {
	a.variables[key] = value
	return nil
}
func (a *apiFeature) iSetVariableAsBool(key string, value string) error {
	v, _ := strconv.ParseBool(value)
	a.variables[key] = v
	return nil
}
func (a *apiFeature) iLoadVariablesFromDirectory(dirname string) error {
	if dirname == "" {
		return fmt.Errorf("No directory name given")
	}

	f, err := os.Open(dirname)
	if err != nil {
		return err
	}
	files, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return err
	}

	for _, file := range files {
		log.Trace().Str("file", file.Name()).Msg("Reading content to memory")
		content, err := ioutil.ReadFile(dirname + "/" + file.Name())
		if err != nil {
			return err
		}
		key := strings.TrimSuffix(file.Name(), ".graphql")
		log.Trace().Str("key", key).Str("value", string(content)).Msg("Setting content to memory")
		a.memory[key] = string(content)
	}
	return nil
}

func (a *apiFeature) theResponseErrorsJqShouldMatchNumber(path string, value int) (err error) {
	var v interface{}
	if string(a.lastErrors) == "" {
		a.lastErrors = []byte(`[]`)
	}
	err = json.Unmarshal(a.lastErrors, &v)
	if err != nil {
		return err
	}
	query, err := gojq.Parse(path)
	if err != nil {
		return err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return err
	}
	var actual int
	iter := code.Run(v)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		switch v.(type) {
		case int:
			actual = v.(int)
			break
		case float64:
			actual = int(v.(float64))
			break
		default:
			return errors.New("Cannot parse value as number")
		}
	}
	if actual != value {
		return fmt.Errorf("No match for value, expected=[%d] got=[%d] for path=[%s]", value, actual, path)
	}
	return nil
}

func (a *apiFeature) iExecuteQuery(key string) error {
	if _, ok := a.memory["GRAPHQL_ENDPOINT"]; ok == false {
		return fmt.Errorf("No graphql endpoint defined. Please set GRAPHQL_ENDPOINT env variable/memory.")
	}
	endpoint := a.memory["GRAPHQL_ENDPOINT"].(string)
	if endpoint == "" {
		return fmt.Errorf("No graphql endpoint defined. Please set GRAPHQL_ENDPOINT env variable/memory.")
	}
	if _, ok := a.memory[key]; ok == false {
		return fmt.Errorf("No graphql query %s defined. Please set memory with a value.", key)
	}
	value := a.memory[key].(string)
	log.Trace().Str("endpoint", endpoint).Str("name", key).Str("body", value).Msg("Executing query")
	c := struct {
		Query     string      `json:"query"`
		Variables interface{} `json:"variables"`
	}{Query: a.memory[key].(string), Variables: a.variables}
	content, err := json.Marshal(c)
	if err != nil {
		return err
	}
	a.headers["Content-Type"] = "application/json"
	err = a.sendrequestTo("POST", endpoint, string(content))
	if err != nil {
		return err
	}
	var resp struct {
		Errors []struct {
			Message string `json:"message,omitempty"`
		} `json:"errors,omitempty"`
	}
	err = json.Unmarshal(a.lastBody, &resp)
	if err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		var errs []string
		for _, v := range resp.Errors {
			errs = append(errs, v.Message)
		}
		a.lastErrors, _ = json.Marshal(resp.Errors)
		//return errors.New(resp.Errors[0].Message)
	}
	return err
}

func (a *apiFeature) iWaitSeconds(value int) (err error) {
	time.Sleep(time.Duration(value * 1e9))
	return nil
}

func (a *apiFeature) iSetHTTPHeaderAs(key, value string) error {
	a.headers[key] = a.getParsed(value)
	return nil
}
func (a *apiFeature) iDumpMemory() error {
	for k, v := range a.memory {
		log.Info().Str("key", k).Str("value", fmt.Sprintf("%#v", v)).Msg("Dumped memory value")
	}
	return nil
}
func (a *apiFeature) iDumpVariables() error {
	for k, v := range a.variables {
		log.Info().Str("key", k).Str("value", fmt.Sprintf("%#v", v)).Msg("Dumped variable")
	}
	return nil
}
func (a *apiFeature) iDumpHeaders() error {
	for k, v := range a.headers {
		log.Info().Str("key", k).Str("value", fmt.Sprintf("%#v", v)).Msg("Dumped header")
	}
	return nil
}
func (a *apiFeature) iDumpResponseHeaders() error {
	for k, v := range a.lastHeaders {
		log.Info().Str("key", k).Str("value", fmt.Sprintf("%#v", v)).Msg("Dumped response header")
	}
	return nil
}
func (a *apiFeature) iDumpResponseAsJSON() error {
	fmt.Println(string(pretty.Color(pretty.Pretty(a.lastBody), nil)))
	return nil
}
func (a *apiFeature) iResetVariables() error {
	a.variables = map[string]interface{}{}
	return nil
}
func (a *apiFeature) iResetHeaders() error {
	a.headers = map[string]string{}
	return nil
}
func (a *apiFeature) iResetMemory() error {
	a.memory = map[string]interface{}{}
	for k, v := range defaultMemory {
		a.memory[k] = v
	}
	return nil
}

func seedDefaultMemory() {
	defaultMemory = map[string]string{}
	//for k, v := range []string{"ENDPOINT", "OWNER", "HOME"} {
	for k, v := range strings.Split(seeded, ",") {
		value := os.Getenv(v)
		if value != "" {
			defaultMemory[v] = value
			log.Debug().Int("k", k).Str("key", v).Str("value", value).Msg("Seeding default memory")
		}
	}
}

func init() {
	flag.StringVar(&seeded, "seeded", seeded, "List of env variables to seed memory")
	godog.BindFlags("", flag.CommandLine, &opt)
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	if err := godotenv.Load(); err != nil {
		log.Warn().Str("TAG", TAG).Str("COMMIT",COMMIT).Msg("File .env not found, reading configuration from ENV")
	}

	LOGLEVEL := os.Getenv("LOGLEVEL")
	switch LOGLEVEL {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
		break
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		break
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		break
	case "warn", "":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		break
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		break
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
		break
	case "none":
		zerolog.SetGlobalLevel(zerolog.NoLevel)
		break
	default:
		log.Warn().Str("LOGLEVEL", LOGLEVEL).Msg("Unsupported level")
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	}

	LOGFORMAT := os.Getenv("LOGFORMAT")
	switch LOGFORMAT {
	case "console":
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
		break
	case "", "json":
		break
	default:
		log.Warn().Str("LOGFORMAT", LOGFORMAT).Msg("Unsupported format")
		break
	}

	FORMAT := os.Getenv("FORMAT")
	switch FORMAT {
	case "pretty", "":
		opt.Format = "pretty"
		break
	case "progress":
		opt.Format = "progress"
		break
	default:
		log.Warn().Str("format", FORMAT).Msg("Unsupported format")
	}

	log.Debug().Str("TAG",TAG).Str("COMMIT",COMMIT).Str("loglevel", LOGLEVEL).Str("logformat", LOGFORMAT).Msg("Starting")
	cookieJar, _ = cookiejar.New(nil)
}

func InitializeScenario(s *godog.ScenarioContext) {
	api := &apiFeature{URL: "http://localhost:9903/api"}

	s.BeforeScenario(api.resetResponse)

	s.Step(`^I send "(GET|POST|PUT|DELETE)" request to "([^"]*)"$`, api.iSendrequestTo)
	s.Step(`^I send "(GET|POST|PUT|DELETE)" request to "([^"]*)" with data:$`, api.iSendrequestToWithData)

	s.Step(`^the response code should be (\d+)$`, api.theResponseCodeShouldBe)
	s.Step(`^the response header "([^"]*)" should match "([^"]*)"$`, api.theResponseHeaderShouldMatch)
	s.Step(`^the response should be:$`, api.theResponseShouldBe)

	s.Step(`^the response should match json:$`, api.theResponseShouldMatchJSON)
	s.Step(`^the response should match subset of json:$`, api.theResponseShouldMatchSubsetOfJSON)

	s.Step(`^the response jsonpath "([^"]*)" should match "([^"]*)"$`, api.theResponseJsonpathShouldMatch)
	s.Step(`^the response jsonpath "([^"]*)" should match number "([^"]*)"$`, api.theResponseJsonpathShouldMatchNumber)
	s.Step(`^the response jsonpath "([^"]*)" should match json:$`, api.theResponseJsonpathShouldMatchJson)
	s.Step(`^the response jsonpath "([^"]*)" should match subset of json:$`, api.theResponseJsonpathShouldMatchSubsetOfJson)

	s.Step(`^the response jq "([^"]*)" should match "([^"]*)"$`, api.theResponseJqShouldMatch)
	s.Step(`^the response jq "([^"]*)" should match number "([^"]*)"$`, api.theResponseJqShouldMatchNumber)
	s.Step(`^the response jq "([^"]*)" should match float "([^"]*)"$`, api.theResponseJqShouldMatchFloat)
	s.Step(`^the response jq "([^"]*)" should match json:$`, api.theResponseJqShouldMatchJson)
	s.Step(`^the response jq "([^"]*)" should match subset of json:$`, api.theResponseJqShouldMatchSubsetOfJson)

	s.Step(`^the response errors should match json:$`, api.theResponseErrorsShouldMatchJSON)
	s.Step(`^the response errors jq "([^"]*)" should match json:$`, api.theResponseErrorsJqShouldMatchJson)
	s.Step(`^the response errors jq "([^"]*)" should match number "([^"]*)"$`, api.theResponseErrorsJqShouldMatchNumber)

	s.Step(`^I remember response jsonpath "([^"]*)" as "([^"]*)"$`, api.iRememberJsonpathAs)
	s.Step(`^I remember jsonpath "([^"]*)" as "([^"]*)"$`, api.iRememberJsonpathAs) //@deprecated  backward compatibility
	s.Step(`^I remember response jq "([^"]*)" as "([^"]*)"$`, api.iRememberJqAs)
	s.Step(`^I remember "([^"]*)" as "([^"]*)"$`, api.iRememberAs)
	s.Step(`^I remember "([^"]*)" as:$`, api.iRememberAsBody)

	s.Step(`^I set variable "([^"]*)" as "([^"]*)"$`, api.iSetVariableAs)
	s.Step(`^I set variable "([^"]*)" as string list "([^"]*)"$`, api.iSetVariableAsStringList)
	s.Step(`^I set variable "([^"]*)" as number "([^"]*)"$`, api.iSetVariableAsNumber)
	s.Step(`^I set variable "([^"]*)" as float "([^"]*)"$`, api.iSetVariableAsFloat)
	s.Step(`^I set variable "([^"]*)" as boolean "([^"]*)"$`, api.iSetVariableAsBool)

	s.Step(`^I set HTTP header "([^"]*)" as "([^"]*)"$`, api.iSetHTTPHeaderAs)

	s.Step(`^I execute query "([^"]*)"$`, api.iExecuteQuery)
	s.Step(`^I wait "([^"]*)" seconds$`, api.iWaitSeconds)

	s.Step(`^I dump memory$`, api.iDumpMemory)
	s.Step(`^I dump variables$`, api.iDumpVariables)
	s.Step(`^I dump headers$`, api.iDumpHeaders)
	s.Step(`^I dump response headers$`, api.iDumpResponseHeaders)
	s.Step(`^I dump response as JSON$`, api.iDumpResponseAsJSON)

	s.Step(`^I reset headers$`, api.iResetHeaders)
	s.Step(`^I reset variables$`, api.iResetVariables)
	s.Step(`^I reset memory$`, api.iResetMemory)

	s.Step(`^I load variables from directory "([^"]*)"$`, api.iLoadVariablesFromDirectory)

}

func After(s string) string {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Error().Err(err).Str("dur", s).Msg("Cannot parse time duration")
		panic(err)
	}
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

func main() {
	flag.Parse()
	funcMap = template.FuncMap{
		"now":   time.Now,
		"after": After,
	}

	seedDefaultMemory()
	opt.Paths = flag.Args()
	status := godog.TestSuite{
		Name:                "godogs",
		ScenarioInitializer: InitializeScenario,
		Options:             &opt,
	}.Run()

	os.Exit(status)
}
