package indexer

import (
	"log/slog"
	"context"
	"net"
	"file-indexer/internal/pb"
	"google.golang.org/grpc"
	"log"
)

type IndexerServer struct {
	pb.UnimplementedIndexerServer
}

func (s *IndexerServer) Index(ctx context.Context, req *pb.IndexRequest) (*pb.IndexResponse, error) {
	slog.Info("handling", "name", req.Name)
	return &pb.IndexResponse{Status: "GOOD"}, nil 
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

