protoc \
	--gogo_out=\
Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types:$GOPATH/src \
	-I /usr/local/include \
	-I . \
	proto/types.proto
