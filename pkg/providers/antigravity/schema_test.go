package antigravity

import (
	"encoding/json"
	"testing"
)

func TestCleanJSONSchemaForGemini(t *testing.T) {
	raw := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "A search pattern"
			},
			"options": {
				"type": "object",
				"properties": {
					"mode": {
						"const": "fast"
					},
					"filter": {
						"oneOf": [
							{"type": "null"},
							{
								"type": "object",
								"properties": {
									"name": {"type": "string", "minLength": 1}
								},
								"propertyNames": {"pattern": "^[a-z]+$"},
								"required": ["name", "nonexistent"]
							}
						]
					}
				},
				"additionalProperties": false
			},
			"emptyObj": {
				"type": "object"
			},
			"strList": {
				"type": "array"
			}
		},
		"required": ["pattern", "invalidField"]
	}`

	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("failed to unmarshal test JSON: %v", err)
	}

	cleaned := CleanJSONSchemaForGemini(schema).(map[string]interface{})

	// Check top level
	if cleaned["$schema"] != nil {
		t.Errorf("expected $schema to be deleted")
	}

	// Pattern property should NOT be deleted as a property name
	props := cleaned["properties"].(map[string]interface{})
	if props["pattern"] == nil {
		t.Errorf("expected properties.pattern to be preserved")
	}

	// required should only contain valid fields
	req := cleaned["required"].([]interface{})
	if len(req) != 1 || req[0] != "pattern" {
		t.Errorf("expected required to be ['pattern'], got: %v", req)
	}

	// options check
	options := props["options"].(map[string]interface{})
	if options["additionalProperties"] != nil {
		t.Errorf("expected additionalProperties to be deleted")
	}
	optProps := options["properties"].(map[string]interface{})

	// mode: const -> enum
	mode := optProps["mode"].(map[string]interface{})
	if mode["const"] != nil {
		t.Errorf("expected const to be deleted")
	}
	if mode["enum"] == nil {
		t.Errorf("expected enum to be created from const")
	}

	// filter: oneOf flattened
	filter := optProps["filter"].(map[string]interface{})
	if filter["oneOf"] != nil {
		t.Errorf("expected oneOf to be deleted")
	}
	if filter["propertyNames"] != nil {
		t.Errorf("expected propertyNames to be deleted")
	}
	filterProps := filter["properties"].(map[string]interface{})
	nameProp := filterProps["name"].(map[string]interface{})
	if nameProp["minLength"] != nil {
		t.Errorf("expected minLength to be deleted")
	}
	filterReq := filter["required"].([]interface{})
	if len(filterReq) != 1 || filterReq[0] != "name" {
		t.Errorf("expected filter.required to be ['name'], got: %v", filterReq)
	}

	// emptyObj placeholder
	emptyObj := props["emptyObj"].(map[string]interface{})
	emptyProps := emptyObj["properties"].(map[string]interface{})
	if emptyProps["reason"] == nil {
		t.Errorf("expected emptyObj to have reason placeholder")
	}

	// strList array items
	strList := props["strList"].(map[string]interface{})
	if strList["items"] == nil {
		t.Errorf("expected strList to have items")
	}
}

func TestPropertiesMapNotCorrupted(t *testing.T) {
	// Replicates tools with property names identical to schema keywords like 'properties', 'required', 'type'
	raw := `{
		"type": "object",
		"properties": {
			"type": {
				"type": "string",
				"description": "type of thing"
			},
			"required": {
				"type": "boolean",
				"description": "is it required"
			},
			"properties": {
				"type": "object"
			},
			"items": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"nested_type": {"type": "string"}
					}
				}
			}
		},
		"if": {"properties": {"type": {"const": "special"}}},
		"then": {"required": ["required"]},
		"else": {"required": ["properties"]}
	}`

	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("failed to unmarshal test JSON: %v", err)
	}

	cleaned := CleanJSONSchemaForGemini(schema).(map[string]interface{})

	// 'if', 'then', 'else' must be deleted
	if cleaned["if"] != nil || cleaned["then"] != nil || cleaned["else"] != nil {
		t.Errorf("if/then/else should be deleted from root schema")
	}

	// properties map must ONLY contain Schema objects, NEVER a string "object"!
	props, ok := cleaned["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is not a map")
	}

	for k, v := range props {
		propSchema, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("property %q value is NOT a map (Schema object)! got: %#v", k, v)
		}
		// Each property schema must have a valid type
		if propSchema["type"] == nil {
			t.Errorf("property %q has nil type", k)
		}
	}
}
