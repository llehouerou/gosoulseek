package requests

import "github.com/llehouerou/gosoulseek/messaging"

type SearchRequest struct {
	SearchText string
	Token      int
}

func NewSearchRequest(searchText string, token int) SearchRequest {
	return SearchRequest{
		SearchText: searchText,
		Token:      token,
	}
}

func (r SearchRequest) ToBytes() []byte {
	return messaging.NewMessageBuilder().
		Code(messaging.ServerFileSearch).
		Integer(r.Token).
		String(r.SearchText).
		Build()
}
