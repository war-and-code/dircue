package scanner

import "io"

// readAllBounded uses a size hint only to reserve space. The reader and byte
// limit determine the returned data, including when a file grows or shrinks.
func readAllBounded(reader io.Reader, limit, sizeHint int64) ([]byte, error) {
	reader = io.LimitReader(reader, limit)
	// Reserve at most one ordinary aggregate prefix, including its lookahead.
	// In particular, a large or stale file size cannot reserve its full size.
	initial := min(sizeHint, limit, maxAttributesBytes+1)
	if initial <= 0 {
		return io.ReadAll(reader)
	}
	data := make([]byte, 0, int(initial))
	for {
		if len(data) == cap(data) {
			if int64(len(data)) == limit {
				return data, nil
			}
			// Probe before growing: accurate size hints commonly end at EOF.
			var next [1]byte
			n, err := reader.Read(next[:])
			if n > 0 {
				growth := min(max(cap(data), 512), int(^uint(0)>>1)-cap(data))
				growth = int(min(int64(growth), limit-int64(cap(data))))
				if sizeHint > int64(cap(data)) {
					growth = int(min(int64(growth), sizeHint-int64(cap(data))))
				}
				grown := make([]byte, len(data), cap(data)+growth)
				copy(grown, data)
				data = append(grown, next[0])
			}
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				return data, err
			}
			continue
		}
		n, err := reader.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
	}
}
