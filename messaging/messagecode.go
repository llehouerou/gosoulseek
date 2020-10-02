package messaging

type MessageCode uint32

const (
	DistributedPing                MessageCode = 0
	DistributedSearchrequest       MessageCode = 3
	DistributedBranchlevel         MessageCode = 4
	DistributedBranchroot          MessageCode = 5
	DistributedUnknown             MessageCode = 6
	DistributedChildDepth          MessageCode = 7
	DistributedServerSearchRequest MessageCode = 93
)

const (
	InitializationPierceFirewall MessageCode = 0
	InitializationPeerInit       MessageCode = 1
)

const (
	PeerBrowseRequest          MessageCode = 4
	PeerBrowseResponse         MessageCode = 5
	PeerSearchRequest          MessageCode = 8
	PeerSearchResponse         MessageCode = 9
	PeerInfoRequest            MessageCode = 15
	PeerInfoResponse           MessageCode = 16
	PeerFolderContentsRequest  MessageCode = 36
	PeerFolderContentsResponse MessageCode = 37
	PeerTransferRequest        MessageCode = 40
	PeerTransferResponse       MessageCode = 41
	PeerUploadPlacehold        MessageCode = 42
	PeerQueueDownload          MessageCode = 43
	PeerPlaceInQueueResponse   MessageCode = 44
	PeerUploadFailed           MessageCode = 46
	PeerQueueFailed            MessageCode = 50
	PeerPlaceInQueueRequest    MessageCode = 51
)

const (
	ServerUnknown                     MessageCode = 0
	ServerLogin                       MessageCode = 1
	ServerSetListenPort               MessageCode = 2
	ServerGetPeerAddress              MessageCode = 3
	ServerAddUser                     MessageCode = 5
	ServerGetStatus                   MessageCode = 7
	ServerSayInChatRoom               MessageCode = 13
	ServerJoinRoom                    MessageCode = 14
	ServerLeaveRoom                   MessageCode = 15
	ServerUserJoinedRoom              MessageCode = 16
	ServerUserLeftRoom                MessageCode = 17
	ServerConnectToPeer               MessageCode = 18
	ServerPrivateMessages             MessageCode = 22
	ServerAcknowledgePrivateMessage   MessageCode = 23
	ServerFileSearch                  MessageCode = 26
	ServerSetOnlineStatus             MessageCode = 28
	ServerPing                        MessageCode = 32
	ServerSendSpeed                   MessageCode = 34
	ServerSharedFoldersAndFiles       MessageCode = 35
	ServerGetUserStats                MessageCode = 36
	ServerQueuedDownloads             MessageCode = 40
	ServerKickedFromServer            MessageCode = 41
	ServerUserSearch                  MessageCode = 42
	ServerInterestAdd                 MessageCode = 51
	ServerInterestRemove              MessageCode = 52
	ServerGetRecommendations          MessageCode = 54
	ServerGetGlobalRecommendations    MessageCode = 56
	ServerGetUserInterests            MessageCode = 57
	ServerRoomList                    MessageCode = 64
	ServerExactFileSearch             MessageCode = 65
	ServerGlobalAdminMessage          MessageCode = 66
	ServerPrivilegedUsers             MessageCode = 69
	ServerHaveNoParents               MessageCode = 71
	ServerParentsIP                   MessageCode = 73
	ServerParentMinSpeed              MessageCode = 83
	ServerParentSpeedRatio            MessageCode = 84
	ServerParentInactivityTimeout     MessageCode = 86
	ServerSearchInactivityTimeout     MessageCode = 87
	ServerMinimumParentsInCache       MessageCode = 88
	ServerDistributedAliveInterval    MessageCode = 90
	ServerAddPrivilegedUser           MessageCode = 91
	ServerCheckPrivileges             MessageCode = 92
	ServerSearchRequest               MessageCode = 93
	ServerAcceptChildren              MessageCode = 100
	ServerNetInfo                     MessageCode = 102
	ServerWishlistSearch              MessageCode = 103
	ServerWishlistInterval            MessageCode = 104
	ServerGetSimilarUsers             MessageCode = 110
	ServerGetItemRecommendations      MessageCode = 111
	ServerGetItemSimilarUsers         MessageCode = 112
	ServerRoomTickers                 MessageCode = 113
	ServerRoomTickerAdd               MessageCode = 114
	ServerRoomTickerRemove            MessageCode = 115
	ServerSetRoomTicker               MessageCode = 116
	ServerHatedInterestAdd            MessageCode = 117
	ServerHatedInterestRemove         MessageCode = 118
	ServerRoomSearch                  MessageCode = 120
	ServerSendUploadSpeed             MessageCode = 121
	ServerUserPrivileges              MessageCode = 122
	ServerGivePrivileges              MessageCode = 123
	ServerNotifyPrivileges            MessageCode = 124
	ServerAcknowledgeNotifyPrivileges MessageCode = 125
	ServerBranchLevel                 MessageCode = 126
	ServerBranchRoot                  MessageCode = 127
	ServerChildDepth                  MessageCode = 129
	ServerPrivateRoomUsers            MessageCode = 133
	ServerPrivateRoomAddUser          MessageCode = 134
	ServerPrivateRoomRemoveUser       MessageCode = 135
	ServerPrivateRoomDropMembership   MessageCode = 136
	ServerPrivateRoomDropOwnership    MessageCode = 137
	ServerPrivateRoomUnknown          MessageCode = 138
	ServerPrivateRoomAdded            MessageCode = 139
	ServerPrivateRoomRemoved          MessageCode = 140
	ServerPrivateRoomToggle           MessageCode = 141
	ServerNewPassword                 MessageCode = 142
	ServerPrivateRoomAddOperator      MessageCode = 143
	ServerPrivateRoomRemoveOperator   MessageCode = 144
	ServerPrivateRoomOperatorAdded    MessageCode = 145
	ServerPrivateRoomOperatorRemoved  MessageCode = 146
	ServerPrivateRoomOwned            MessageCode = 148
	ServerMessageUsers                MessageCode = 149
	ServerAskPublicChat               MessageCode = 150
	ServerStopPublicChat              MessageCode = 151
	ServerPublicChat                  MessageCode = 152
	ServerCannotConnect               MessageCode = 1001
)
