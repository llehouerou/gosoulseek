package server_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestSetListenPort_Code(t *testing.T) {
	m := &server.SetListenPort{Port: 50000}
	assert.Equal(t, protocol.ServerSetListenPort, m.Code())
}

func TestSetListenPort_Encode(t *testing.T) {
	tests := []struct {
		name string
		port uint32
		want []byte
	}{
		{
			name: "standard port",
			port: 50000,
			want: []byte{
				0x02, 0x00, 0x00, 0x00, // code: 2
				0x50, 0xC3, 0x00, 0x00, // port: 50000
			},
		},
		{
			name: "zero port",
			port: 0,
			want: []byte{
				0x02, 0x00, 0x00, 0x00, // code: 2
				0x00, 0x00, 0x00, 0x00, // port: 0
			},
		},
		{
			name: "max port",
			port: 65535,
			want: []byte{
				0x02, 0x00, 0x00, 0x00, // code: 2
				0xFF, 0xFF, 0x00, 0x00, // port: 65535
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &server.SetListenPort{Port: tt.port}
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)
			m.Encode(w)
			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}

func TestSetOnlineStatus_Code(t *testing.T) {
	m := &server.SetOnlineStatus{Status: server.StatusOnline}
	assert.Equal(t, protocol.ServerSetOnlineStatus, m.Code())
}

func TestSetOnlineStatus_Encode(t *testing.T) {
	tests := []struct {
		name   string
		status server.UserPresence
		want   []byte
	}{
		{
			name:   "offline",
			status: server.StatusOffline,
			want: []byte{
				0x1C, 0x00, 0x00, 0x00, // code: 28
				0x00, 0x00, 0x00, 0x00, // status: 0
			},
		},
		{
			name:   "away",
			status: server.StatusAway,
			want: []byte{
				0x1C, 0x00, 0x00, 0x00, // code: 28
				0x01, 0x00, 0x00, 0x00, // status: 1
			},
		},
		{
			name:   "online",
			status: server.StatusOnline,
			want: []byte{
				0x1C, 0x00, 0x00, 0x00, // code: 28
				0x02, 0x00, 0x00, 0x00, // status: 2
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &server.SetOnlineStatus{Status: tt.status}
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)
			m.Encode(w)
			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}

func TestUserPresence_String(t *testing.T) {
	tests := []struct {
		status server.UserPresence
		want   string
	}{
		{server.StatusOffline, "Offline"},
		{server.StatusAway, "Away"},
		{server.StatusOnline, "Online"},
		{server.UserPresence(99), "Unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.status.String())
	}
}

func TestSharedFoldersAndFiles_Code(t *testing.T) {
	m := &server.SharedFoldersAndFiles{Directories: 10, Files: 100}
	assert.Equal(t, protocol.ServerSharedFoldersAndFiles, m.Code())
}

func TestSharedFoldersAndFiles_Encode(t *testing.T) {
	tests := []struct {
		name string
		dirs uint32
		file uint32
		want []byte
	}{
		{
			name: "zero shares",
			dirs: 0,
			file: 0,
			want: []byte{
				0x23, 0x00, 0x00, 0x00, // code: 35
				0x00, 0x00, 0x00, 0x00, // dirs: 0
				0x00, 0x00, 0x00, 0x00, // files: 0
			},
		},
		{
			name: "some shares",
			dirs: 10,
			file: 250,
			want: []byte{
				0x23, 0x00, 0x00, 0x00, // code: 35
				0x0A, 0x00, 0x00, 0x00, // dirs: 10
				0xFA, 0x00, 0x00, 0x00, // files: 250
			},
		},
		{
			name: "large shares",
			dirs: 1000,
			file: 50000,
			want: []byte{
				0x23, 0x00, 0x00, 0x00, // code: 35
				0xE8, 0x03, 0x00, 0x00, // dirs: 1000
				0x50, 0xC3, 0x00, 0x00, // files: 50000
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &server.SharedFoldersAndFiles{Directories: tt.dirs, Files: tt.file}
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)
			m.Encode(w)
			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}
