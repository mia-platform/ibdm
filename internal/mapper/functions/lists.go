// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"fmt"
	"reflect"
)

// List creates a list from the provided elements.
func List(elements ...any) []any {
	return elements
}

func Append(list any, elements ...any) ([]any, error) {
	listType := reflect.TypeOf(list).Kind()
	switch listType {
	case reflect.Slice, reflect.Array:
		reflectedList := reflect.ValueOf(list)
		result := make([]any, reflectedList.Len(), reflectedList.Len()+len(elements))
		for i := range reflectedList.Len() {
			result[i] = reflectedList.Index(i).Interface()
		}

		return append(result, elements...), nil
	default:
		return nil, fmt.Errorf("cannot append to type %s", listType.String())
	}
}

func Prepend(list any, elements ...any) ([]any, error) {
	listType := reflect.TypeOf(list).Kind()
	switch listType {
	case reflect.Slice, reflect.Array:
		reflectedList := reflect.ValueOf(list)
		result := make([]any, reflectedList.Len(), reflectedList.Len()+len(elements))
		for i := range reflectedList.Len() {
			result[i] = reflectedList.Index(i).Interface()
		}

		return append(elements, result...), nil
	default:
		return nil, fmt.Errorf("cannot prepend to type %s", listType.String())
	}
}

// First returns the first element of the provided list. If the list is empty, it returns nil.
// If the provided argument is not a list, it returns an error.
func First(list any) (any, error) {
	listType := reflect.TypeOf(list).Kind()
	switch listType {
	case reflect.Slice, reflect.Array:
		reflectedList := reflect.ValueOf(list)
		if reflectedList.Len() == 0 {
			return nil, nil
		}
		return reflectedList.Index(0).Interface(), nil
	case reflect.String:
		str := reflect.ValueOf(list).String()
		if len(str) == 0 {
			return nil, nil
		}
		return string(str[0]), nil
	default:
		return nil, fmt.Errorf("cannot find first element of type %s", listType.String())
	}
}

// Last returns the last element of the provided list. If the list is empty, it returns nil.
// If the provided argument is not a list, it returns an error.
func Last(list any) (any, error) {
	listType := reflect.TypeOf(list).Kind()
	switch listType {
	case reflect.Slice, reflect.Array:
		reflectedList := reflect.ValueOf(list)
		length := reflectedList.Len()
		if length == 0 {
			return nil, nil
		}
		return reflectedList.Index(length - 1).Interface(), nil
	case reflect.String:
		str := reflect.ValueOf(list).String()
		length := len(str)
		if length == 0 {
			return nil, nil
		}
		return string(str[length-1]), nil
	default:
		return nil, fmt.Errorf("cannot find last element of type %s", listType.String())
	}
}
