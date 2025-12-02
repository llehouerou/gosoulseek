package server_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestFileSearch_Code(t *testing.T) {
	m := &server.FileSearch{Token: 1, Query: "test"}
	assert.Equal(t, protocol.ServerFileSearch, m.Code())
}

func TestFileSearch_Encode(t *testing.T) {
	tests := []struct {
		name  string
		token uint32
		query string
		want  []byte
	}{
		{
			name:  "simple query",
			token: 12345,
			query: "test",
			want: []byte{
				0x1A, 0x00, 0x00, 0x00, // code: 26
				0x39, 0x30, 0x00, 0x00, // token: 12345
				0x04, 0x00, 0x00, 0x00, // query length: 4
				't', 'e', 's', 't', // query
			},
		},
		{
			name:  "empty query",
			token: 1,
			query: "",
			want: []byte{
				0x1A, 0x00, 0x00, 0x00, // code: 26
				0x01, 0x00, 0x00, 0x00, // token: 1
				0x00, 0x00, 0x00, 0x00, // query length: 0
			},
		},
		{
			name:  "complex query",
			token: 99999,
			query: "artist album mp3",
			want: []byte{
				0x1A, 0x00, 0x00, 0x00, // code: 26
				0x9F, 0x86, 0x01, 0x00, // token: 99999
				0x10, 0x00, 0x00, 0x00, // query length: 16
				'a', 'r', 't', 'i', 's', 't', ' ',
				'a', 'l', 'b', 'u', 'm', ' ',
				'm', 'p', '3',
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &server.FileSearch{Token: tt.token, Query: tt.query}
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)
			m.Encode(w)
			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}
