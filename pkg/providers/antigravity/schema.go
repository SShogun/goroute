package antigravity

import (
	"strings"
)

// unsupportedGeminiKeys contains JSON Schema keys not supported by Google Gemini/Cloud Code API
var unsupportedGeminiKeys = map[string]bool{
	"$schema":               true,
	"$id":                   true,
	"$defs":                 true,
	"definitions":           true,
	"const":                 true,
	"$ref":                  true,
	"propertyNames":         true,
	"patternProperties":     true,
	"unevaluatedProperties": true,
	"unevaluatedItems":      true,
	"uniqueItems":           true,
	"contains":              true,
	"minContains":           true,
	"maxContains":           true,
	"anyOf":                 true,
	"oneOf":                 true,
	"allOf":                 true,
	"not":                   true,
	"dependencies":          true,
	"dependentSchemas":      true,
	"dependentRequired":     true,
	"minLength":             true,
	"maxLength":             true,
	"exclusiveMinimum":      true,
	"exclusiveMaximum":      true,
	"multipleOf":            true,
	"minItems":              true,
	"maxItems":              true,
	"format":                true,
	"default":               true,
	"examples":              true,
	"title":                 true,
	"strict":                true,
	"encrypted":             true,
	"deprecated":            true,
	"optional":              true,
	"additionalProperties":  true,
}

// CleanJSONSchemaForGemini recursively cleans a JSON Schema to comply with Gemini OpenAPI 3.0 subset
// Implements the 8-phase transformation battle-tested in OmniRoute / CLIProxyAPI.
func CleanJSONSchemaForGemini(schema interface{}) interface{} {
	if schema == nil {
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Brief explanation of why you are calling this tool",
				},
			},
			"required": []interface{}{"reason"},
		}
	}

	root, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}

	// Deep clone first
	cloned := deepClone(root).(map[string]interface{})

	// Phase 1: Convert const to enum & enum values to strings
	phase1ConvertConstAndEnums(cloned)

	// Phase 2: Merge allOf & flatten anyOf/oneOf & flatten type arrays
	phase2FlattenStructures(cloned)

	// Phase 3: Remove unsupported keywords recursively
	phase3RemoveUnsupportedKeywords(cloned)

	// Phase 4: Clean required fields recursively
	phase4CleanRequired(cloned)

	// Phase 5: Ensure object schemas have properties & placeholders
	phase5AddPlaceholders(cloned)

	// Phase 6: Ensure array items schema
	phase6EnsureArrayItems(cloned)

	return cloned
}

func deepClone(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		res := make(map[string]interface{}, len(val))
		for k, item := range val {
			res[k] = deepClone(item)
		}
		return res
	case []interface{}:
		res := make([]interface{}, len(val))
		for i, item := range val {
			res[i] = deepClone(item)
		}
		return res
	default:
		return v
	}
}

// Phase 1: Convert const to enum and ensure enum is []string
func phase1ConvertConstAndEnums(obj interface{}) {
	if obj == nil {
		return
	}
	switch v := obj.(type) {
	case map[string]interface{}:
		if cVal, hasConst := v["const"]; hasConst && v["enum"] == nil {
			v["enum"] = []interface{}{cVal}
			delete(v, "const")
		}
		if enumList, ok := v["enum"].([]interface{}); ok {
			if v["type"] == "integer" || v["type"] == "number" {
				delete(v, "enum")
			} else {
				strEnums := make([]interface{}, len(enumList))
				for i, e := range enumList {
					if s, ok := e.(string); ok {
						strEnums[i] = s
					} else {
						strEnums[i] = strings.Trim(string(mustMarshal(e)), "\"")
					}
				}
				v["enum"] = strEnums
				if v["type"] == nil {
					v["type"] = "string"
				}
			}
		}
		for _, child := range v {
			phase1ConvertConstAndEnums(child)
		}
	case []interface{}:
		for _, item := range v {
			phase1ConvertConstAndEnums(item)
		}
	}
}

// Phase 2: Merge allOf, flatten anyOf/oneOf, flatten type arrays
func phase2FlattenStructures(obj interface{}) {
	if obj == nil {
		return
	}
	switch v := obj.(type) {
	case map[string]interface{}:
		// Merge allOf
		if allOf, ok := v["allOf"].([]interface{}); ok && len(allOf) > 0 {
			mergedProps := make(map[string]interface{})
			var mergedReq []interface{}
			for _, item := range allOf {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if props, ok := itemMap["properties"].(map[string]interface{}); ok {
						for pk, pv := range props {
							mergedProps[pk] = pv
						}
					}
					if req, ok := itemMap["required"].([]interface{}); ok {
						mergedReq = append(mergedReq, req...)
					}
				}
			}
			delete(v, "allOf")
			if len(mergedProps) > 0 {
				existingProps, _ := v["properties"].(map[string]interface{})
				if existingProps == nil {
					existingProps = make(map[string]interface{})
				}
				for pk, pv := range mergedProps {
					existingProps[pk] = pv
				}
				v["properties"] = existingProps
			}
			if len(mergedReq) > 0 {
				existingReq, _ := v["required"].([]interface{})
				v["required"] = append(existingReq, mergedReq...)
			}
		}

		// Flatten anyOf / oneOf: select the best non-null schema
		flattenBranch := func(key string) {
			if branches, ok := v[key].([]interface{}); ok && len(branches) > 0 {
				var nonNull []map[string]interface{}
				for _, b := range branches {
					if bm, ok := b.(map[string]interface{}); ok && bm["type"] != "null" {
						nonNull = append(nonNull, bm)
					}
				}
				if len(nonNull) > 0 {
					best := selectBestSchema(nonNull)
					delete(v, key)
					for bk, bv := range best {
						v[bk] = bv
					}
				} else {
					delete(v, key)
				}
			}
		}
		flattenBranch("anyOf")
		flattenBranch("oneOf")

		// Flatten type arrays: ["string", "null"] -> "string"
		if types, ok := v["type"].([]interface{}); ok && len(types) > 0 {
			chosen := "string"
			for _, t := range types {
				if s, ok := t.(string); ok && s != "null" {
					chosen = s
					break
				}
			}
			v["type"] = chosen
		}

		for _, child := range v {
			phase2FlattenStructures(child)
		}
	case []interface{}:
		for _, item := range v {
			phase2FlattenStructures(item)
		}
	}
}

func selectBestSchema(schemas []map[string]interface{}) map[string]interface{} {
	bestIdx := 0
	bestScore := -1
	for i, s := range schemas {
		score := 0
		t, _ := s["type"].(string)
		if t == "object" || s["properties"] != nil {
			score = 3
		} else if t == "array" || s["items"] != nil {
			score = 2
		} else if t != "" && t != "null" {
			score = 1
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return schemas[bestIdx]
}

// Phase 3: Remove unsupported keywords recursively.
// Descend into each property's subschema, but never delete property names themselves!
func phase3RemoveUnsupportedKeywords(obj interface{}) {
	if obj == nil {
		return
	}
	switch v := obj.(type) {
	case map[string]interface{}:
		for k := range v {
			if unsupportedGeminiKeys[k] || strings.HasPrefix(k, "x-") {
				delete(v, k)
			}
		}
		for k, child := range v {
			if k == "properties" {
				if props, ok := child.(map[string]interface{}); ok {
					for _, propSchema := range props {
						phase3RemoveUnsupportedKeywords(propSchema)
					}
					continue
				}
			}
			phase3RemoveUnsupportedKeywords(child)
		}
	case []interface{}:
		for _, item := range v {
			phase3RemoveUnsupportedKeywords(item)
		}
	}
}

// Phase 4: Clean required fields recursively
func phase4CleanRequired(obj interface{}) {
	if obj == nil {
		return
	}
	switch v := obj.(type) {
	case map[string]interface{}:
		if req, ok := v["required"].([]interface{}); ok {
			props, _ := v["properties"].(map[string]interface{})
			var valid []interface{}
			for _, r := range req {
				if rStr, ok := r.(string); ok && props != nil {
					if _, exists := props[rStr]; exists {
						valid = append(valid, rStr)
					}
				}
			}
			if len(valid) > 0 {
				v["required"] = valid
			} else {
				delete(v, "required")
			}
		}
		for _, child := range v {
			phase4CleanRequired(child)
		}
	case []interface{}:
		for _, item := range v {
			phase4CleanRequired(item)
		}
	}
}

// Phase 5: Ensure type: "object" has placeholder if properties are empty
func phase5AddPlaceholders(obj interface{}) {
	if obj == nil {
		return
	}
	switch v := obj.(type) {
	case map[string]interface{}:
		// Inject type:"object" if missing but properties or required present
		if v["type"] == nil && (v["properties"] != nil || v["required"] != nil) {
			v["type"] = "object"
		}
		if v["type"] == "object" {
			props, _ := v["properties"].(map[string]interface{})
			if len(props) == 0 {
				v["properties"] = map[string]interface{}{
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Brief explanation of why you are calling this tool",
					},
				}
				v["required"] = []interface{}{"reason"}
			}
		}
		for _, child := range v {
			phase5AddPlaceholders(child)
		}
	case []interface{}:
		for _, item := range v {
			phase5AddPlaceholders(item)
		}
	}
}

// Phase 6: Ensure array items schema
func phase6EnsureArrayItems(obj interface{}) {
	if obj == nil {
		return
	}
	switch v := obj.(type) {
	case map[string]interface{}:
		if v["type"] == "array" && v["items"] == nil {
			v["items"] = map[string]interface{}{"type": "string"}
		}
		for _, child := range v {
			phase6EnsureArrayItems(child)
		}
	case []interface{}:
		for _, item := range v {
			phase6EnsureArrayItems(item)
		}
	}
}

func mustMarshal(v interface{}) []byte {
	switch s := v.(type) {
	case string:
		return []byte(s)
	default:
		return []byte("")
	}
}
