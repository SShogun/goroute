package kiro

// NormalizeKiroToolSchema recursively cleans schema for Kiro API:
// - Removes additionalProperties (Kiro rejects it)
// - Removes empty required array (Kiro rejects required: [])
func NormalizeKiroToolSchema(schema interface{}) interface{} {
	if schema == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}

	m, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}

	result := make(map[string]interface{})
	for k, v := range m {
		if k == "additionalProperties" {
			continue
		}
		if k == "required" {
			if reqList, ok := v.([]interface{}); ok && len(reqList) == 0 {
				continue
			}
		}
		if k == "properties" {
			if props, ok := v.(map[string]interface{}); ok {
				cleanProps := make(map[string]interface{})
				for pk, pv := range props {
					cleanProps[pk] = NormalizeKiroToolSchema(pv)
				}
				result[k] = cleanProps
				continue
			}
		}
		if subMap, ok := v.(map[string]interface{}); ok {
			result[k] = NormalizeKiroToolSchema(subMap)
		} else if arr, ok := v.([]interface{}); ok {
			cleanArr := make([]interface{}, len(arr))
			for i, item := range arr {
				cleanArr[i] = NormalizeKiroToolSchema(item)
			}
			result[k] = cleanArr
		} else {
			result[k] = v
		}
	}

	return result
}
