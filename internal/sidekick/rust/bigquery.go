// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rust

import (
	"fmt"
	"slices"
	"strings"

	"github.com/googleapis/librarian/internal/sidekick/api"
)

// bigQuerySetter holds the structured metadata for a single generated builder method.
type bigQuerySetter struct {
	MethodName string
	DocLine    string

	// Type properties
	PrimType   string
	KeyType    string
	ValueType  string
	IsMap      bool
	IsRepeated bool
	IsOrClear  bool
	IsCopy     bool

	// Routing/Execution properties
	HasQueryRequest          bool
	HasJobConfigurationQuery  bool
	HasJobConfiguration       bool
}

// generateBigQuerySetters compiles the forwarding setters for the unified RunQuery builder.
func generateBigQuerySetters(model *api.API) ([]bigQuerySetter, error) {
	var qrMsg, jcqMsg, jcMsg *api.Message

	// Find the target low-level messages in the model
	for _, msg := range model.Messages {
		if msg.Name == "QueryRequest" {
			qrMsg = msg
		} else if msg.Name == "JobConfigurationQuery" {
			jcqMsg = msg
		} else if msg.Name == "JobConfiguration" {
			jcMsg = msg
		}
	}

	if qrMsg == nil || jcqMsg == nil || jcMsg == nil {
		return nil, fmt.Errorf("failed to locate QueryRequest, JobConfigurationQuery, or JobConfiguration messages")
	}

	// Index fields by their name
	qrFields := make(map[string]*api.Field)
	for _, f := range qrMsg.Fields {
		qrFields[f.Name] = f
	}

	jcqFields := make(map[string]*api.Field)
	for _, f := range jcqMsg.Fields {
		jcqFields[f.Name] = f
	}

	jcFields := make(map[string]*api.Field)
	for _, f := range jcMsg.Fields {
		jcFields[f.Name] = f
	}

	// Collect all unique field names across the three models
	var allFieldNames []string
	for name := range qrFields {
		allFieldNames = append(allFieldNames, name)
	}
	for name := range jcqFields {
		allFieldNames = append(allFieldNames, name)
	}
	for name := range jcFields {
		allFieldNames = append(allFieldNames, name)
	}
	slices.Sort(allFieldNames)
	allFieldNames = slices.Compact(allFieldNames)

	skippedFields := []string{"query", "kind", "job_type", "copy", "load", "extract", "format_options"}
	var setters []bigQuerySetter

	for _, fieldName := range allFieldNames {
		if slices.Contains(skippedFields, fieldName) {
			continue
		}

		qrF := qrFields[fieldName]
		jcqF := jcqFields[fieldName]
		jcF := jcFields[fieldName]

		isOutputOnly := func(f *api.Field) bool {
			if f == nil {
				return true
			}
			return slices.Contains(f.Behavior, api.FieldBehaviorOutputOnly)
		}

		if isOutputOnly(qrF) && isOutputOnly(jcqF) && isOutputOnly(jcF) {
			continue
		}


		// Generate normal and or_clear setters where applicable
		variations := []struct {
			suffix    string
			isOrClear bool
		}{
			{suffix: "", isOrClear: false},
			{suffix: "_or_clear", isOrClear: true},
		}

		for _, v := range variations {
			methodName := fmt.Sprintf("set%s_%s", v.suffix, fieldName)

			// Determine which model targets have this setter variation
			var hasQr, hasJcq, hasJc bool
			if v.isOrClear {
				hasQr = qrF != nil && qrF.Optional
				hasJcq = jcqF != nil && jcqF.Optional
				hasJc = jcF != nil && jcF.Optional
			} else {
				hasQr = qrF != nil
				hasJcq = jcqF != nil
				hasJc = jcF != nil
			}

			if !hasQr && !hasJcq && !hasJc {
				continue
			}

			// Get the primary field and its annotations for types
			var primaryField *api.Field
			if qrF != nil {
				primaryField = qrF
			} else if jcqF != nil {
				primaryField = jcqF
			} else {
				primaryField = jcF
			}

			fAnn := primaryField.Codec.(*fieldAnnotations)

			primType := strings.ReplaceAll(fAnn.PrimitiveFieldType, "crate::model", "google_cloud_bigquery_v2::model")
			keyType := strings.ReplaceAll(fAnn.KeyType, "crate::model", "google_cloud_bigquery_v2::model")
			valType := strings.ReplaceAll(fAnn.ValueType, "crate::model", "google_cloud_bigquery_v2::model")

			// Build the list of reference links to all targets that support this setter
			var links []string
			if hasQr {
				links = append(links, fmt.Sprintf("[%s][google_cloud_bigquery_v2::model::QueryRequest::%s]", fieldName, fieldName))
			}
			if hasJcq {
				links = append(links, fmt.Sprintf("[%s][google_cloud_bigquery_v2::model::JobConfigurationQuery::%s]", fieldName, fieldName))
			}
			if hasJc {
				links = append(links, fmt.Sprintf("[%s][google_cloud_bigquery_v2::model::JobConfiguration::%s]", fieldName, fieldName))
			}

			var docLine string
			if v.isOrClear {
				docLine = fmt.Sprintf("Sets or clears the value of %s.", strings.Join(links, " and "))
			} else {
				docLine = fmt.Sprintf("Sets the value of %s.", strings.Join(links, " and "))
			}

			// primitive and wkt wrapper types in Rust implementing the Copy trait
			isCopy := primType == "bool" || primType == "i32" || primType == "i64" || primType == "f64" ||
				primType == "wkt::BoolValue" || primType == "wkt::Int32Value" || primType == "wkt::Int64Value" ||
				primType == "wkt::UInt32Value" || primType == "wkt::UInt64Value" || primType == "wkt::FloatValue" ||
				primType == "wkt::DoubleValue"

			setters = append(setters, bigQuerySetter{
				MethodName:               methodName,
				DocLine:                  docLine,
				PrimType:                 primType,
				KeyType:                  keyType,
				ValueType:                valType,
				IsMap:                    primaryField.Map,
				IsRepeated:               primaryField.Repeated,
				IsOrClear:                v.isOrClear,
				IsCopy:                   isCopy,
				HasQueryRequest:          hasQr,
				HasJobConfigurationQuery:  hasJcq,
				HasJobConfiguration:       hasJc,
			})
		}
	}

	return setters, nil
}
