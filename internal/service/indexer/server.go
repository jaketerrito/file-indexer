package indexer

import (
	"log/slog"
	"net"
	"net/http"
	"file-indexer/internal/pb"
	"google.golang.org/grpc"
	"log"
	"io"
	"google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

type IndexerServer struct {
	pb.UnimplementedIndexerServer
}

func (s *IndexerServer) Index(stream grpc.ClientStreamingServer[pb.IndexRequest, pb.IndexResponse]) error {
	var metadata *pb.FileMetadata
	var contentType string

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break // DONE
		}
		if err != nil {
			return err
		}

		if metadata == nil {
			metaReq, ok := req.GetData().(*pb.IndexRequest_Metadata)
			if !ok {
				return status.Errorf(codes.InvalidArgument, "protocol violation: the first stream message must be 'metadata'")
			}
			metadata = metaReq.Metadata

			// Should have something to create a buffer or something for reading the file
			continue
        }

		// Read file contents
		contentReq, ok := req.GetData().(*pb.IndexRequest_Content)
        if !ok {
			return status.Errorf(codes.InvalidArgument, "protocol violation: content missing")
        }

		if contentType == "" {
			// Content Type can be determined with first 512 bytes of a file
			limit := min(512, len(contentReq.Content))
			contentType = http.DetectContentType(contentReq.Content[:limit])
		}
        // Do something with the content
		//slog.Info("Data", "content", contentReq.Content)
	}

	slog.Info("handling", "name", metadata.Name, "content type", contentType)
	return stream.SendAndClose(&pb.IndexResponse{Status: "GOOD"})
}

func (s *IndexerServer) Run(addr string) error {
	lis, err:= net.Listen("tcp", addr)

	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterIndexerServer(grpcServer, s)
	log.Printf("listening on %s", addr)
	return grpcServer.Serve(lis)
}

