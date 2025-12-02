package protocol

// ServerCode identifies a server message type.
// Server messages use a 4-byte code.
type ServerCode uint32

// Server message codes.
// Reference: Soulseek.NET/src/Messaging/MessageCode.cs
const (
	ServerLogin                  ServerCode = 1
	ServerSetListenPort          ServerCode = 2
	ServerGetPeerAddress         ServerCode = 3
	ServerWatchUser              ServerCode = 5
	ServerUnwatchUser            ServerCode = 6
	ServerGetStatus              ServerCode = 7
	ServerSayInChatRoom          ServerCode = 13
	ServerJoinRoom               ServerCode = 14
	ServerLeaveRoom              ServerCode = 15
	ServerUserJoinedRoom         ServerCode = 16
	ServerUserLeftRoom           ServerCode = 17
	ServerConnectToPeer          ServerCode = 18
	ServerPrivateMessage         ServerCode = 22
	ServerAcknowledgePrivateMsg  ServerCode = 23
	ServerFileSearch             ServerCode = 26
	ServerSetOnlineStatus        ServerCode = 28
	ServerPing                   ServerCode = 32
	ServerSendSpeed              ServerCode = 34
	ServerSharedFoldersAndFiles  ServerCode = 35
	ServerGetUserStats           ServerCode = 36
	ServerKickedFromServer       ServerCode = 41
	ServerUserSearch             ServerCode = 42
	ServerRoomList               ServerCode = 64
	ServerGlobalAdminMessage     ServerCode = 66
	ServerPrivilegedUsers        ServerCode = 69
	ServerHaveNoParents          ServerCode = 71
	ServerParentsIP              ServerCode = 73
	ServerCheckPrivileges        ServerCode = 92
	ServerEmbeddedMessage        ServerCode = 93
	ServerAcceptChildren         ServerCode = 100
	ServerNetInfo                ServerCode = 102
	ServerWishlistSearch         ServerCode = 103
	ServerRoomTickers            ServerCode = 113
	ServerRoomSearch             ServerCode = 120
	ServerSendUploadSpeed        ServerCode = 121
	ServerUserPrivileges         ServerCode = 122
	ServerGivePrivileges         ServerCode = 123
	ServerNotifyPrivileges       ServerCode = 124
	ServerBranchLevel            ServerCode = 126
	ServerBranchRoot             ServerCode = 127
	ServerChildDepth             ServerCode = 129
	ServerDistributedReset       ServerCode = 130
	ServerPrivateRoomUsers       ServerCode = 133
	ServerPrivateRoomAddUser     ServerCode = 134
	ServerPrivateRoomRemoveUser  ServerCode = 135
	ServerPrivateRoomToggle      ServerCode = 141
	ServerNewPassword            ServerCode = 142
	ServerPrivateRoomAddOperator ServerCode = 143
	ServerPublicChat             ServerCode = 152
	ServerCannotConnect          ServerCode = 1001
)

// PeerCode identifies a peer message type.
// Peer messages use a 4-byte code.
type PeerCode uint32

// Peer message codes.
const (
	PeerBrowseRequest         PeerCode = 4
	PeerBrowseResponse        PeerCode = 5
	PeerSearchResponse        PeerCode = 9
	PeerInfoRequest           PeerCode = 15
	PeerInfoResponse          PeerCode = 16
	PeerFolderContentsRequest PeerCode = 36
	PeerFolderContentsResp    PeerCode = 37
	PeerTransferRequest       PeerCode = 40
	PeerTransferResponse      PeerCode = 41
	PeerQueueDownload         PeerCode = 43
	PeerPlaceInQueueResponse  PeerCode = 44
	PeerUploadFailed          PeerCode = 46
	PeerUploadDenied          PeerCode = 50
	PeerPlaceInQueueRequest   PeerCode = 51
)

// DistributedCode identifies a distributed network message type.
// Distributed messages use a 1-byte code.
type DistributedCode uint8

// Distributed message codes.
const (
	DistributedPing            DistributedCode = 0
	DistributedSearchRequest   DistributedCode = 3
	DistributedBranchLevel     DistributedCode = 4
	DistributedBranchRoot      DistributedCode = 5
	DistributedChildDepth      DistributedCode = 7
	DistributedEmbeddedMessage DistributedCode = 93
)

// InitCode identifies a connection initialization message type.
// Initialization messages use a 1-byte code.
type InitCode uint8

// Initialization message codes.
const (
	InitPierceFirewall InitCode = 0
	InitPeerInit       InitCode = 1
)
