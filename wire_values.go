package gocraft

import (
	"fmt"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

func playerFrom(value abi.Value, sink *effects) (*PlayerRef, error) {
	items, err := listOf(value, 3, "player")
	if err != nil {
		return nil, err
	}
	if items[0].Kind != abi.ValueBytes || len(items[0].Bytes) != 16 {
		return nil, fmt.Errorf("gocraft: player uuid is not 16 bytes")
	}
	username, err := stringFrom(items[1], "player username")
	if err != nil {
		return nil, err
	}
	edition, err := stringFrom(items[2], "player edition")
	if err != nil {
		return nil, err
	}
	player := &PlayerRef{Username: username, Edition: edition, sink: sink}
	copy(player.UUID[:], items[0].Bytes)
	return player, nil
}

func positionFrom(value abi.Value) (BlockPos, error) {
	items, err := listOf(value, 3, "block position")
	if err != nil {
		return BlockPos{}, err
	}
	for _, item := range items {
		if item.Kind != abi.ValueInt64 {
			return BlockPos{}, fmt.Errorf("gocraft: block position coordinate is not an integer")
		}
	}
	return BlockPos{X: items[0].Int64, Y: items[1].Int64, Z: items[2].Int64}, nil
}

func blockFrom(value abi.Value) (Block, error) {
	items, err := listOf(value, 2, "block")
	if err != nil {
		return Block{}, err
	}
	id, err := stringFrom(items[0], "block id")
	if err != nil {
		return Block{}, err
	}
	properties, err := propertiesFrom(items[1])
	if err != nil {
		return Block{}, err
	}
	return Block{ID: id, Properties: properties}, nil
}
