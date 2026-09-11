package gocraft

import (
	"reflect"
	"strings"
	"testing"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
	wire "github.com/GoCraft-MC/gocraft-abi/abi/v1/wire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Reflection is confined to this schema-driven test, never the dispatch path.
func TestEveryNativeSchemaMapping(t *testing.T) {
	messages := wire.File_abi_v1_events_proto.Messages()
	count := 0
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)
		if !proto.HasExtension(message.Options(), wire.E_Event) {
			continue
		}
		count++
		options := proto.GetExtension(message.Options(), wire.E_Event).(*wire.EventOptions)
		t.Run(options.Type, func(t *testing.T) {
			fields := schemaValues(t, message)
			event, err := eventFrom(&abi.Event{Type: options.Type, Fields: fields}, &effects{})
			if err != nil || event.Type() != options.Type || nativeCancellable(options.Type) != options.Cancellable {
				t.Fatalf("schema mapping: %T, %v", event, err)
			}
			if changes := nativeMutations(event, fields); len(changes) != 0 {
				t.Fatalf("unchanged decode produced mutations: %+v", changes)
			}
			if _, err := eventFrom(&abi.Event{Type: options.Type, Fields: append(fields, abi.String("extra"))}, &effects{}); err == nil {
				t.Fatal("accepted wrong field count")
			}
			for j := 0; j < message.Fields().Len(); j++ {
				field := message.Fields().Get(j)
				if !proto.GetExtension(field.Options(), wire.E_Mutable).(bool) {
					continue
				}
				fresh, err := eventFrom(&abi.Event{Type: options.Type, Fields: fields}, &effects{})
				if err != nil {
					t.Fatal(err)
				}
				value := reflect.ValueOf(fresh).Elem().FieldByNameFunc(func(name string) bool {
					return strings.EqualFold(name, strings.ReplaceAll(string(field.Name()), "_", ""))
				})
				switch field.Kind() {
				case protoreflect.StringKind:
					value.SetString("modified")
				case protoreflect.DoubleKind:
					value.SetFloat(42.5)
				default:
					t.Fatalf("add mutable fixture for %s", field.Kind())
				}
				changes := nativeMutations(fresh, fields)
				if len(changes) != 1 || len(changes[0].Path) != 1 || changes[0].Path[0] != uint32(j) {
					t.Fatalf("%s mutation mapping: %+v", field.Name(), changes)
				}
				after, err := abi.ApplyPath(fields, changes[0])
				if err != nil || abi.Equal(after[j], fields[j]) {
					t.Fatalf("%s mutation did not return: %v", field.Name(), err)
				}
			}
		})
	}
	if count < 14 {
		t.Fatalf("only %d native schemas tested", count)
	}
}

func schemaValues(t *testing.T, message protoreflect.MessageDescriptor) []abi.Value {
	t.Helper()
	values := make([]abi.Value, message.Fields().Len())
	for i := range values {
		field := message.Fields().Get(i)
		switch {
		case field.IsMap():
			values[i] = abi.List()
		case field.Kind() == protoreflect.MessageKind:
			values[i] = abi.List(schemaValues(t, field.Message())...)
		case field.Kind() == protoreflect.StringKind:
			values[i] = abi.String("fixture")
		case field.Kind() == protoreflect.BytesKind:
			values[i] = abi.Bytes(make([]byte, 16))
		case field.Kind() == protoreflect.Sint64Kind:
			values[i] = abi.Int64(int64(i + 1))
		case field.Kind() == protoreflect.DoubleKind:
			values[i] = abi.Double(float64(i) + 0.25)
		default:
			t.Fatalf("add fixture for %s", field.Kind())
		}
	}
	return values
}
