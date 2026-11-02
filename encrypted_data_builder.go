package dgproto

// NewEncryptedData constructs a validated application envelope with independently
// owned fields and values. It copies the field slice and every Value, including
// fields built with struct literals; callers may reuse or mutate their inputs
// after return. Inputs must not be mutated concurrently with this call.
//
// Validation and error priority match EncryptedData.EncodedLen. Invalid input
// returns the zero envelope without copying values. StreamID, AppMessageType and
// unknown field types remain application-defined; no authorization or schema
// validation is implied. Empty fields are valid.
//
// The returned envelope is mutable, not frozen. Keep it and its reachable buffers
// immutable while queued, including after a canceled write-wait. Copying the
// returned struct alone does not create another independent envelope; call this
// constructor again to snapshot existing fields. Use ValidateFrameSize with the
// actual padding before sending: construction does not reserve queue capacity or
// account for random frame padding, and is not a send or delivery guarantee.
func NewEncryptedData(streamID uint16, appMessageType uint8, fields ...TLV) (EncryptedData, error) {
	message := EncryptedData{StreamID: streamID, AppMessageType: appMessageType, Fields: fields}
	if _, err := message.EncodedLen(); err != nil {
		return EncryptedData{}, err
	}
	if fields != nil {
		message.Fields = make([]TLV, len(fields))
		for i, field := range fields {
			message.Fields[i] = TLV{Type: field.Type, Value: append([]byte(nil), field.Value...)}
		}
	}
	return message, nil
}
