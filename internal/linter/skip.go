package linter

func newSkipSet(skipRules []string) map[string]struct{} {
	if len(skipRules) == 0 {
		return nil
	}

	skipSet := make(map[string]struct{}, len(skipRules))
	for _, code := range skipRules {
		if code == "" {
			continue
		}
		skipSet[code] = struct{}{}
	}
	if len(skipSet) == 0 {
		return nil
	}
	return skipSet
}

func filterRuleCodes(ruleCodes []string, skipSet map[string]struct{}) []string {
	if len(skipSet) == 0 {
		return ruleCodes
	}

	filtered := ruleCodes[:0]
	for _, code := range ruleCodes {
		if _, skip := skipSet[code]; skip {
			continue
		}
		filtered = append(filtered, code)
	}
	return filtered
}

func isSkipped(ruleCode string, skipSet map[string]struct{}) bool {
	if len(skipSet) == 0 {
		return false
	}
	_, skip := skipSet[ruleCode]
	return skip
}
