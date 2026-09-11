package gocraft

import (
	"fmt"
	"math"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

func boolFrom(value abi.Value, description string) (bool, error) {
	if value.Kind != abi.ValueBool {
		return false, fmt.Errorf("gocraft: %s is not a boolean", description)
	}
	return value.Bool, nil
}

func int64From(value abi.Value, description string) (int64, error) {
	if value.Kind != abi.ValueInt64 {
		return 0, fmt.Errorf("gocraft: %s is not an integer", description)
	}
	return value.Int64, nil
}

func doubleFrom(value abi.Value, description string) (float64, error) {
	if value.Kind != abi.ValueDouble || math.IsNaN(value.Double) || math.IsInf(value.Double, 0) {
		return 0, fmt.Errorf("gocraft: %s is not a finite number", description)
	}
	return value.Double, nil
}

func bytesFrom(value abi.Value, description string) ([]byte, error) {
	if value.Kind != abi.ValueBytes {
		return nil, fmt.Errorf("gocraft: %s is not bytes", description)
	}
	return append([]byte(nil), value.Bytes...), nil
}
