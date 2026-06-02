package services

func missingAStockFields(intent string, data map[string]interface{}) []string {
	missing := []string{}
	for _, field := range requiredAStockFields(intent) {
		if aStockFieldAvailable(field, data) {
			continue
		}
		missing = append(missing, field)
	}
	return missing
}

func aStockFieldAvailable(field string, data map[string]interface{}) bool {
	switch field {
	case "latest_price":
		return hasMapValue(data, "latest_price") || hasMapValue(data, "price")
	case "main_net_inflow":
		return hasMapValue(data, "main_net_inflow") || hasMapValue(data, "latest_main_net")
	case "net_buy_amount":
		return hasNestedRows(data, "records", field) || hasNestedRows(data, "items", field)
	case "broker_seat", "buy_amount", "sell_amount":
		return hasNestedRows(data, "buy_seats", field) || hasNestedRows(data, "sell_seats", field)
	default:
		return hasMapValue(data, field) || hasNestedRows(data, "records", field) || hasNestedRows(data, "items", field)
	}
}

func hasMapValue(data map[string]interface{}, key string) bool {
	value, ok := data[key]
	if !ok || value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return true
}

func hasNestedRows(data map[string]interface{}, rowsKey string, field string) bool {
	rows, ok := data[rowsKey].([]map[string]interface{})
	if !ok || len(rows) == 0 {
		return false
	}
	return hasMapValue(rows[0], field)
}

func latestTradeDate(items []map[string]interface{}) string {
	if len(items) == 0 {
		return ""
	}
	if value, ok := items[len(items)-1]["trade_date"].(string); ok {
		return value
	}
	return ""
}
