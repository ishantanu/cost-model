package prom

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/kubecost/cost-model/pkg/log"
	"github.com/kubecost/cost-model/pkg/util"
)

var (
	// Static Warnings for data point parsing
	InfWarning warning = newWarning("Found Inf value parsing vector data point for metric")
	NaNWarning warning = newWarning("Found NaN value parsing vector data point for metric")

	// Static Errors for query result parsing
	QueryResultNilErr          error = NewCommError("nil queryResult")
	PromUnexpectedResponseErr  error = errors.New("Unexpected response from Prometheus")
	DataFieldFormatErr         error = errors.New("Data field improperly formatted in prometheus repsonse")
	ResultFieldDoesNotExistErr error = errors.New("Result field not does not exist in prometheus response")
	ResultFieldFormatErr       error = errors.New("Result field improperly formatted in prometheus response")
	ResultFormatErr            error = errors.New("Result is improperly formatted")
	MetricFieldDoesNotExistErr error = errors.New("Metric field does not exist in data result vector")
	MetricFieldFormatErr       error = errors.New("Metric field is improperly formatted")
	ValueFieldDoesNotExistErr  error = errors.New("Value field does not exist in data result vector")
	ValuesFieldFormatErr       error = errors.New("Values field is improperly formatted")
	DataPointFormatErr         error = errors.New("Improperly formatted datapoint from Prometheus")
	NoDataErr                  error = errors.New("No data")
)

// QueryResultsChan is a channel of query results
type QueryResultsChan chan *QueryResults

// Await returns query results, blocking until they are made available, and
// deferring the closure of the underlying channel
func (qrc QueryResultsChan) Await() *QueryResults {
	defer close(qrc)
	return <-qrc
}

// QueryResult contains a single result from a prometheus query. It's common
// to refer to query results as a slice of QueryResult
type QueryResult struct {
	Metric map[string]interface{}
	Values []*util.Vector
}

// QueryResults contains all of the query results and the source query string.
type QueryResults struct {
	Query   string
	Results []*QueryResult
}

// NewQueryResults accepts the raw prometheus query result and returns an array of
// QueryResult objects
func NewQueryResults(query string, queryResult interface{}) (*QueryResults, error) {
	if queryResult == nil {
		return nil, QueryResultNilErr
	}

	data, ok := queryResult.(map[string]interface{})["data"]
	if !ok {
		e, err := wrapPrometheusError(queryResult)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf(e)
	}

	// Deep Check for proper formatting
	d, ok := data.(map[string]interface{})
	if !ok {
		return nil, DataFieldFormatErr
	}
	resultData, ok := d["result"]
	if !ok {
		return nil, ResultFieldDoesNotExistErr
	}
	resultsData, ok := resultData.([]interface{})
	if !ok {
		return nil, ResultFieldFormatErr
	}

	// Result vectors from the query
	var results []*QueryResult

	// Parse raw results and into QueryResults
	for _, val := range resultsData {
		resultInterface, ok := val.(map[string]interface{})
		if !ok {
			return nil, ResultFormatErr
		}

		metricInterface, ok := resultInterface["metric"]
		if !ok {
			return nil, MetricFieldDoesNotExistErr
		}
		metricMap, ok := metricInterface.(map[string]interface{})
		if !ok {
			return nil, MetricFieldFormatErr
		}

		// Define label string for values to ensure that we only run labelsForMetric once
		// if we receive multiple warnings.
		var labelString string = ""

		// Determine if the result is a ranged data set or single value
		_, isRange := resultInterface["values"]

		var vectors []*util.Vector
		if !isRange {
			dataPoint, ok := resultInterface["value"]
			if !ok {
				return nil, ValueFieldDoesNotExistErr
			}

			// Append new data point, log warnings
			v, warn, err := parseDataPoint(dataPoint)
			if err != nil {
				return nil, err
			}
			if warn != nil {
				log.Warningf("%s\nQuery: %s\nLabels: %s", warn.Message(), query, labelsForMetric(metricMap))
			}

			vectors = append(vectors, v)
		} else {
			values, ok := resultInterface["values"].([]interface{})
			if !ok {
				return nil, ValuesFieldFormatErr
			}

			// Append new data points, log warnings
			for _, value := range values {
				v, warn, err := parseDataPoint(value)
				if err != nil {
					return nil, err
				}
				if warn != nil {
					if labelString == "" {
						labelString = labelsForMetric(metricMap)
					}
					log.Warningf("%s\nQuery: %s\nLabels: %s", warn.Message(), query, labelString)
				}

				vectors = append(vectors, v)
			}
		}

		results = append(results, &QueryResult{
			Metric: metricMap,
			Values: vectors,
		})
	}

	return &QueryResults{
		Query:   query,
		Results: results,
	}, nil
}

func (qrs *QueryResults) GetFirstValue() (float64, error) {
	if len(qrs.Results) == 0 {
		return 0.0, NoDataErr
	}

	if len(qrs.Results[0].Values) == 0 {
		return 0, ResultFormatErr
	}

	return qrs.Results[0].Values[0].Value, nil
}

// GetString returns the requested field, or an error if it does not exist
func (qr *QueryResult) GetString(field string) (string, error) {
	f, ok := qr.Metric[field]
	if !ok {
		return "", fmt.Errorf("'%s' field does not exist in data result vector", field)
	}

	strField, ok := f.(string)
	if !ok {
		return "", fmt.Errorf("'%s' field is improperly formatted", field)
	}

	return strField, nil
}

// GetLabels returns all labels and their values from the query result
func (qr *QueryResult) GetLabels() map[string]string {
	result := make(map[string]string)

	// Find All keys with prefix label_, remove prefix, add to labels
	for k, v := range qr.Metric {
		if !strings.HasPrefix(k, "label_") {
			continue
		}

		label := k[6:]
		value, ok := v.(string)
		if !ok {
			log.Warningf("Failed to parse label value for label: '%s'", label)
			continue
		}

		result[label] = value
	}

	return result
}

// parseDataPoint parses a data point from raw prometheus query results and returns
// a new Vector instance containing the parsed data along with any warnings or errors.
func parseDataPoint(dataPoint interface{}) (*util.Vector, warning, error) {
	var w warning = nil

	value, ok := dataPoint.([]interface{})
	if !ok || len(value) != 2 {
		return nil, w, DataPointFormatErr
	}

	strVal := value[1].(string)
	v, err := strconv.ParseFloat(strVal, 64)
	if err != nil {
		return nil, w, err
	}

	// Test for +Inf and -Inf (sign: 0), Test for NaN
	if math.IsInf(v, 0) {
		w = InfWarning
		v = 0.0
	} else if math.IsNaN(v) {
		w = NaNWarning
		v = 0.0
	}

	return &util.Vector{
		Timestamp: math.Round(value[0].(float64)/10) * 10,
		Value:     v,
	}, w, nil
}

func labelsForMetric(metricMap map[string]interface{}) string {
	var pairs []string
	for k, v := range metricMap {
		pairs = append(pairs, fmt.Sprintf("%s: %+v", k, v))
	}

	return fmt.Sprintf("{%s}", strings.Join(pairs, ", "))
}

func wrapPrometheusError(qr interface{}) (string, error) {
	e, ok := qr.(map[string]interface{})["error"]
	if !ok {
		return "", PromUnexpectedResponseErr
	}
	eStr, ok := e.(string)
	return eStr, nil
}
