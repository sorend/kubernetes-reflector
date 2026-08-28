package mirror

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

var (
	labelNameRegex      = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,61}[A-Za-z0-9])?$`)
	labelPrefixRegex    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	labelValueRegex     = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9._-]{0,61}[A-Za-z0-9])?)?$`)
	setBasedRequirement = regexp.MustCompile(`^(\S+)\s+(in|notin)\s*\(([^)]*)\)$`)
)

type requirementOp string

const (
	opEquals       requirementOp = "equals"
	opNotEquals    requirementOp = "notEquals"
	opIn           requirementOp = "in"
	opNotIn        requirementOp = "notIn"
	opExists       requirementOp = "exists"
	opDoesNotExist requirementOp = "doesNotExist"
)

type requirement struct {
	key    string
	op     requirementOp
	values []string
}

func LabelSelectorMatch(selectorStr string, ns *corev1.Namespace) bool {
	if strings.TrimSpace(selectorStr) == "" {
		return true
	}
	if ns == nil {
		return false
	}

	reqs, errs := parseSelector(selectorStr)
	if len(errs) > 0 {
		return false
	}

	labels := ns.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	for _, req := range reqs {
		value, has := labels[req.key]
		switch req.op {
		case opEquals:
			if !has || value != req.values[0] {
				return false
			}
		case opNotEquals:
			if has && value == req.values[0] {
				return false
			}
		case opIn:
			if !has || !contains(req.values, value) {
				return false
			}
		case opNotIn:
			if has && contains(req.values, value) {
				return false
			}
		case opExists:
			if !has {
				return false
			}
		case opDoesNotExist:
			if has {
				return false
			}
		default:
			return false
		}
	}

	return true
}

func GetLabelSelectorErrors(annotationName, selectorStr string) []string {
	if strings.TrimSpace(selectorStr) == "" {
		return nil
	}

	_, errs := parseSelector(selectorStr)
	if len(errs) == 0 {
		return nil
	}

	result := make([]string, 0, len(errs))
	for _, err := range errs {
		if annotationName == "" {
			result = append(result, err)
			continue
		}
		result = append(result, fmt.Sprintf("%s %q: %s", annotationName, selectorStr, err))
	}
	return result
}

func parseSelector(raw string) ([]requirement, []string) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := splitRequirements(raw)
	if len(parts) == 0 {
		return nil, []string{"selector is not empty but contains no requirements"}
	}

	reqs := make([]requirement, 0, len(parts))
	errs := make([]string, 0)
	for _, part := range parts {
		if req, handled, err := parseSetBased(part); handled {
			if err != "" {
				errs = append(errs, err)
			} else {
				reqs = append(reqs, req)
			}
			continue
		}

		if req, handled, err := parseInequality(part); handled {
			if err != "" {
				errs = append(errs, err)
			} else {
				reqs = append(reqs, req)
			}
			continue
		}

		if req, handled, err := parseEquality(part); handled {
			if err != "" {
				errs = append(errs, err)
			} else {
				reqs = append(reqs, req)
			}
			continue
		}

		if req, handled, err := parseExistence(part); handled {
			if err != "" {
				errs = append(errs, err)
			} else {
				reqs = append(reqs, req)
			}
			continue
		}

		errs = append(errs, fmt.Sprintf("requirement %q could not be parsed", part))
	}

	if len(errs) > 0 {
		return nil, errs
	}

	return reqs, nil
}

func splitRequirements(selectorStr string) []string {
	parts := make([]string, 0)
	depth := 0
	start := 0
	for i, r := range selectorStr {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(selectorStr[start:i])
				if part != "" {
					parts = append(parts, part)
				}
				start = i + 1
			}
		}
	}

	last := strings.TrimSpace(selectorStr[start:])
	if last != "" {
		parts = append(parts, last)
	}

	return parts
}

func parseSetBased(part string) (requirement, bool, string) {
	matches := setBasedRequirement.FindStringSubmatch(part)
	if matches == nil {
		return requirement{}, false, ""
	}

	key := strings.TrimSpace(matches[1])
	op := matches[2]
	rawValues := strings.Split(matches[3], ",")
	values := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		values = append(values, value)
	}

	if !isValidLabelKey(key) {
		return requirement{}, true, fmt.Sprintf("invalid label key %q in set-based requirement", key)
	}
	if len(values) == 0 {
		return requirement{}, true, fmt.Sprintf("set-based requirement for key %q has no values", key)
	}
	for _, value := range values {
		if !isValidLabelValue(value) {
			return requirement{}, true, fmt.Sprintf("invalid label value %q for key %q", value, key)
		}
	}

	req := requirement{key: key, values: values}
	if op == "in" {
		req.op = opIn
	} else {
		req.op = opNotIn
	}
	return req, true, ""
}

func parseInequality(part string) (requirement, bool, string) {
	if !strings.Contains(part, "!=") {
		return requirement{}, false, ""
	}

	pieces := strings.SplitN(part, "!=", 2)
	key := strings.TrimSpace(pieces[0])
	value := strings.TrimSpace(pieces[1])
	if !isValidLabelKey(key) {
		return requirement{}, true, fmt.Sprintf("invalid label key %q in inequality requirement", key)
	}
	if !isValidLabelValue(value) {
		return requirement{}, true, fmt.Sprintf("invalid label value %q for key %q", value, key)
	}

	return requirement{key: key, op: opNotEquals, values: []string{value}}, true, ""
}

func parseEquality(part string) (requirement, bool, string) {
	doubleEq := strings.Index(part, "==")
	singleEq := strings.Index(part, "=")
	if doubleEq < 0 && singleEq < 0 {
		return requirement{}, false, ""
	}

	eqIndex := singleEq
	opLen := 1
	if doubleEq >= 0 {
		eqIndex = doubleEq
		opLen = 2
	}

	key := strings.TrimSpace(part[:eqIndex])
	value := strings.TrimSpace(part[eqIndex+opLen:])
	if !isValidLabelKey(key) {
		return requirement{}, true, fmt.Sprintf("invalid label key %q in equality requirement", key)
	}
	if !isValidLabelValue(value) {
		return requirement{}, true, fmt.Sprintf("invalid label value %q for key %q", value, key)
	}

	return requirement{key: key, op: opEquals, values: []string{value}}, true, ""
}

func parseExistence(part string) (requirement, bool, string) {
	key := strings.TrimSpace(part)
	op := opExists
	if strings.HasPrefix(key, "!") {
		key = strings.TrimSpace(key[1:])
		op = opDoesNotExist
	}

	if !isValidLabelKey(key) {
		return requirement{}, true, fmt.Sprintf("invalid label key %q in existence requirement", key)
	}

	return requirement{key: key, op: op}, true, ""
}

func isValidLabelKey(key string) bool {
	if key == "" {
		return false
	}

	prefix := ""
	name := key
	if slash := strings.Index(key, "/"); slash >= 0 {
		prefix = key[:slash]
		name = key[slash+1:]
		if prefix == "" || len(prefix) > 253 || !labelPrefixRegex.MatchString(prefix) {
			return false
		}
	}

	if name == "" || len(name) > 63 {
		return false
	}

	return labelNameRegex.MatchString(name)
}

func isValidLabelValue(value string) bool {
	if len(value) > 63 {
		return false
	}
	return labelValueRegex.MatchString(value)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
