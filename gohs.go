package gohs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var hsAPIkey string
var errThreshold = 1
var cl = &http.Client{}

const coreURL = "https://api.hubapi.com"

//SetAPIKey Sets the API key for all subsequent functions to use
func SetAPIKey(key string) {
	hsAPIkey = key
}

//SetErrorThresholds Sets the Error Thresholds
func SetErrorThresholds(et int) {
	errThreshold = et
}

func checkKeyPresence() bool {
	if hsAPIkey != "" {
		return true
	}
	return false
}

//HSRequest is the request handler object for Hubspot
type HSRequest struct {
	//Checked properties to advise which request function to activate
	loaded bool

	//HTTP Method Type: GET,POST,PUT Etc....
	httpMethod string

	//Request Headers: Content-Type etc...
	headers []requestHeader

	//Param Values
	paramURL map[string]string

	URL       *url.URL
	paramsURL url.Values

	//
	body []byte
	//http request success response code
	successCode int

	hasOffset                bool
	offsetIdentifier         string
	returnedOffsetIdentifier string

	useLimitAsOffset bool
	limit            string
	limitIdentifier  string
	totalIndentifier string
}

type requestHeader struct {
	name  string
	value string
}

func (rH *requestHeader) addHeader(req *http.Request) {
	req.Header.Set(rH.name, rH.value)
}

//Load pass the parameters required to perform the request
func (hsR *HSRequest) Load(method, bodyURL, body, offsetIdentifier, returnedOffsetIdentifier, totalIndentifier, limitIdentifier, limit string, headerNames, headerValues, parameterNames, parameterValues []string, successCode int, useLimitAsOffset bool) error {
	var (
		err error
	)
	if !checkKeyPresence() {
		return errors.New("no API key present")
	}

	if successCode == 0 || successCode >= 600 {
		return errors.New("http success code invalid")
	}
	hsR.successCode = successCode

	//Construct URL
	if method == "" {
		return errors.New("no http method set")
	}
	hsR.httpMethod = method

	//offset situation
	if offsetIdentifier != "" {
		hsR.offsetIdentifier = offsetIdentifier
		hsR.returnedOffsetIdentifier = returnedOffsetIdentifier

		hsR.hasOffset = true
		hsR.useLimitAsOffset = useLimitAsOffset
	}

	//limit offset situation
	if hsR.useLimitAsOffset {
		hsR.limit = limit
		hsR.limitIdentifier = limitIdentifier
		hsR.totalIndentifier = totalIndentifier

		hsR.useLimitAsOffset = useLimitAsOffset
	}

	//Load Headers
	if err = hsR.loadRequestHeaders(headerNames, headerValues); err != nil {
		if err != nil {
			return createError("loadHeaders: ", err)
		}
	}

	//Construct URL
	hsR.URL, _ = url.Parse(coreURL)
	if bodyURL == "" {
		return errors.New("no url body set")
	}
	hsR.URL.Path += bodyURL

	//Define parameters
	hsR.paramsURL = url.Values{}
	//Add the API to parameters
	hsR.paramsURL.Add("hapikey", hsAPIkey)
	//Load URL Parameters
	if err = hsR.loadURLParameters(parameterNames, parameterValues); err != nil {
		if err != nil {
			return createError("loadURLParameters: ", err)
		}
	}

	if body != "" {
		hsR.body = []byte(body)
	} else {
		hsR.body = nil
	}

	hsR.loaded = true
	return nil
}

func (hsR *HSRequest) loadRequestHeaders(names, headers []string) error {
	if len(names) != len(headers) {
		return errors.New("header name/value array lengths do not match")
	}
	for i := range names {
		hsR.headers = append(hsR.headers, requestHeader{names[i], headers[i]})
	}
	return nil
}
func (hsR *HSRequest) loadURLParameters(names, parameters []string) error {
	if len(names) != len(parameters) {
		return errors.New("header name/parameter array lengths do not match")
	}
	for i := range names {
		hsR.paramsURL.Add(names[i], parameters[i])
	}
	return nil
}

//Do will utilise the loaded parameters to perform
func (hsR *HSRequest) Do() (interface{}, error) {
	//Check that config was loaded
	if !hsR.loaded {
		return nil, errors.New("no config loaded")
	}

	var (
		data interface{}
		err  error
	)

	if hsR.hasOffset {
		if data, err = hsR.DoLoopRequest(); err != nil {
			return nil, createError("hsR.DoLoopRequest: ", err)
		}
	} else {
		//No offset is present therefor you only do a single request
		var (
			req  *http.Request
			resp *http.Response

			retry      = true
			dataoutput = make(map[string]interface{})
			errcount   int
		)

		for retry {

			hsR.URL.RawQuery = hsR.paramsURL.Encode()

			fmt.Println(hsR.URL.String())

			if req, err = http.NewRequest(hsR.httpMethod, hsR.URL.String(), bytes.NewBuffer(hsR.body)); err != nil {
				fmt.Println(err)
				errcount++
				if errcount >= errThreshold {
					return nil, createError("http.NewRequest: ", err)
				}
			} else {
				//Takes object hsR stored headers and applying to the request
				applyHeadersToRequest(hsR, req)
				//Do the request
				if resp, err = cl.Do(req); err != nil {
					fmt.Println(err)
					errcount++
					if errcount >= errThreshold {
						return nil, createError("cl.Do: ", err)
					}
				} else {
					//Passes response code to handler
					if retry, err = hsR.ErrorCodeHandler(resp); err != nil {
						fmt.Println(err)
						errcount++
						if errcount >= errThreshold {
							return nil, createError("hsR.ErrorCodeHandler: ", err)
						}
					} else {
						//Handling the body of the response
						body, _ := ioutil.ReadAll(resp.Body)
						//Checking if the body has data inside
						if len(body) > 0 {
							if err = json.Unmarshal(body, &dataoutput); err != nil {
								fmt.Println(err)
								errcount++
								if errcount >= errThreshold {
									return nil, createError("json.Unmarshal: ", err)
								}
							} else {
								retry = false
								data = dataoutput
							}
						} else {
							retry = false
						}
					}
				}
			}
		}
	}
	return data, nil
}

func (hsR *HSRequest) DoLoopRequest() ([]map[string]interface{}, error) {

	var (
		data []map[string]interface{}

		req  *http.Request
		resp *http.Response

		err      error
		errcount int

		offset     = "0"
		pastoffset string

		total = -1

		hasmore = true
		retry   bool
	)

	hsR.URL.RawQuery = hsR.paramsURL.Encode()

	for hasmore {
		retry = true

		for retry {

			var (
				dataoutput   = make(map[string]interface{})
				offsetString string
			)
			fmt.Println(hsR.paramsURL, len(hsR.paramsURL))
			if len(hsR.paramsURL) > 0 {
				offsetString = "&" + hsR.offsetIdentifier + "=" + offset
			} else {
				offsetString = "?" + hsR.offsetIdentifier + "=" + offset
			}

			fmt.Println(hsR.URL.String() + offsetString)

			if req, err = http.NewRequest(hsR.httpMethod, hsR.URL.String()+offsetString, bytes.NewBuffer(hsR.body)); err != nil {
				fmt.Println(err)
				errcount++
				if errcount >= errThreshold {
					return nil, createError("http.NewRequest: ", err)
				}
			} else {
				applyHeadersToRequest(hsR, req)

				if resp, err = cl.Do(req); err != nil {
					fmt.Println(err)
					errcount++
					if errcount >= errThreshold {
						return nil, createError("cl.Do: ", err)
					}
				} else {

					//Passes response code to handler
					if retry, err = hsR.ErrorCodeHandler(resp); err != nil {
						fmt.Println(err)
						errcount++
						if errcount >= errThreshold {
							return nil, createError("hsR.ErrorCodeHandler: ", err)
						}
					} else {
						//Handling the body of the response
						body, _ := ioutil.ReadAll(resp.Body)
						//Checking if the body has data inside
						if len(body) > 0 {

							if err = json.Unmarshal(body, &dataoutput); err != nil {
								fmt.Println(err)
								errcount++
								if errcount >= errThreshold {
									return nil, createError("json.Unmarshal: ", err)
								}
							} else {
								//shift past offset
								pastoffset = offset

								if !hsR.useLimitAsOffset {
									//get the offset value
									offset = iterateSearchForOffset(dataoutput, hsR.returnedOffsetIdentifier)
									fmt.Println(offset)
									//handle offset
									if offset == "" {
										//fmt.Println(string(body))
										return nil, errors.New("offset not found")
									}
								} else {
									if total < 0 {
										totalT := iterateSearchForOffset(dataoutput, hsR.totalIndentifier)
										total, err = strconv.Atoi(totalT)
									}
									if offset, err = addIntegerToStringInteger(offset, hsR.limit); err != nil {
										return nil, createError("addIntegerToStringInteger: ", err)
									}
								}
								retry = false
								data = append(data, dataoutput)

							}
						} else {
							return nil, errors.New("request body not found")
						}
					}
				}

			}
		}
		//true 0 250 false 0 1
		fmt.Println("before: ", hasmore, pastoffset, offset, retry, total, len(data))
		if !hsR.useLimitAsOffset {
			if (pastoffset != "0" && offset == "0") || pastoffset == offset {
				hasmore = false
			}
		} else {
			var offsetNum int
			if offsetNum, err = strconv.Atoi(offset); err != nil {
				return nil, createError("unable to convert limit offset to int", err)
			}
			if offsetNum >= total {
				hasmore = false
			}
		}
		fmt.Println("after: ", hasmore, pastoffset, offset, retry, total, len(data))

	}

	return data, nil
}

func iterateSearchForOffset(obj map[string]interface{}, pname string) string {
	var (
		exists bool
		valueT interface{}
	)
	//Search current map for offset property
	if valueT, exists = obj[pname]; exists {
		return fmt.Sprintf("%.0f", valueT.(float64))
	}

	//Iterate to find embedded maps
	for _, v := range obj {
		//Perform recurisve search for object
		if nobj, ok := v.(map[string]interface{}); ok {
			if value := iterateSearchForOffset(nobj, pname); value != "" {
				return value
			}
		}
	}

	return ""
}

func applyHeadersToRequest(hsR *HSRequest, req *http.Request) {
	for i := range hsR.headers {
		req.Header.Set(hsR.headers[i].name, hsR.headers[i].value)
	}
}

/*

	Error Handler

*/

var (
	//ErrExcessRequest Avaliable error type that indicates that you have exceeded rate limits
	ErrExcessRequest = errors.New("429 rate limit requests exceeded")
)

//ErrorCodeHandler Returns an informative errors based upon the error code passed
func (hsR *HSRequest) ErrorCodeHandler(resp *http.Response) (bool, error) {
	if resp.StatusCode != hsR.successCode {
		switch resp.StatusCode {
		case 401:
			return false, errors.New("401 authentication invalid")
		case 403:
			return false, errors.New("403 authentication permissions insufficient")
		case 404:
			return false, errors.New("404 unkown endpoint")
		case 415:
			return false, errors.New("415 unsupported media type")
		case 429:
			return false, ErrExcessRequest
		case 502, 504:
			time.Sleep(2 * time.Second)
			return true, errors.New(strconv.Itoa(resp.StatusCode) + " timeout")
		case 500:
			time.Sleep(1 * time.Second)
			return true, errors.New("internal server error")
		default:
			body, _ := ioutil.ReadAll(resp.Body)
			fmt.Println(string(body))
			return true, errors.New("unhandled code: " + strconv.Itoa(resp.StatusCode))
		}
	}
	return true, nil
}

/*

	Additional Convenience Functions

*/

func createError(s string, err error) error {
	return errors.New(s + err.Error())
}

func addIntegerToStringInteger(s1, s2 string) (string, error) {
	n1, err := strconv.Atoi(s1)
	if err != nil {
		return "", err
	}
	n2, err := strconv.Atoi(s2)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(n1 + n2), nil
}

func compareStringIntGreaterEqual(s1, s2 string) (bool, error) {
	n1, err := strconv.Atoi(s1)
	if err != nil {
		return false, err
	}
	n2, err := strconv.Atoi(s2)
	if err != nil {
		return false, err
	}
	if n1 >= n2 {
		return true, nil
	}
	return false, nil
}

//SimplifyInterface Will take the route provided in the [] and append the [] to an existing array
func SimplifyInterface(data []map[string]interface{}, route []string) ([]interface{}, error) {
	var base []interface{}
	for i := 0; i < len(data); i++ {
		indexdata, err := iterateInterface(data[i], route, 0)
		if err != nil {
			return nil, errors.New("iterateInterface: " + err.Error())
		}
		base = append(base, indexdata...)
	}
	return base, nil
}

func iterateInterface(data map[string]interface{}, route []string, index int) ([]interface{}, error) {
	if value, exists := data[route[index]]; exists {
		if index == len(route)-1 {
			if _, ok := value.([]interface{}); !ok {
				return nil, errors.New("unable to convert " + route[index] + " index to []interface{}")
			}
			return value.([]interface{}), nil
		}
		index++
		if _, ok := value.(map[string]interface{}); !ok {
			return nil, errors.New("unable to convert " + route[index] + " index to map[string]interface{}")
		}
		return iterateInterface(value.(map[string]interface{}), route, index)
	}
	return nil, errors.New(route[index] + " does not exist in map")
}

//ConvertIFCArrayToIFCMap Converts an []interface{} assumes it is all map[string]interface{} and returns an array
func ConvertIFCArrayToIFCMap(in []interface{}) (out []map[string]interface{}, err error) {
	for i := range in {
		var outV map[string]interface{}
		var ok bool
		if outV, ok = in[i].(map[string]interface{}); !ok {
			err = errors.New("ConvertIFCArrayToIFCMap: unable to convert to IFC Array")
			return
		}
		out = append(out, outV)
	}
	return
}
