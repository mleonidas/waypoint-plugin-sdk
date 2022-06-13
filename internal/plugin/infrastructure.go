package plugin

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/go-argmapper"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	empty "google.golang.org/protobuf/types/known/emptypb"

	"github.com/hashicorp/waypoint-plugin-sdk/component"
	"github.com/hashicorp/waypoint-plugin-sdk/docs"
	"github.com/hashicorp/waypoint-plugin-sdk/internal/funcspec"
	"github.com/hashicorp/waypoint-plugin-sdk/internal/pluginargs"
	"github.com/hashicorp/waypoint-plugin-sdk/internal/plugincomponent"
	pb "github.com/hashicorp/waypoint-plugin-sdk/proto/gen"
)

// InfraPlugin implements plugin.Plugin (specifically GRPCPlugin) for
// the Infra component type.
type InfraPlugin struct {
	plugin.NetRPCUnsupportedPlugin

	Impl    component.Infra   // Impl is the concrete implementation
	Mappers []*argmapper.Func // Mappers
	Logger  hclog.Logger      // Logger

	ODR *ODRSetting // Used to switch infra modes based on ondemand-runner in play
}

func (p *InfraPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	base := &base{
		Mappers: p.Mappers,
		Logger:  p.Logger,
		Broker:  broker,
	}

	pb.RegisterInfrastructureServer(s, &infraServer{
		base: base,
		Impl: p.Impl,

		authenticatorServer: &authenticatorServer{
			base: base,
			Impl: p.Impl,
		},
	})
	return nil
}

func (p *InfraPlugin) GRPCClient(
	ctx context.Context,
	broker *plugin.GRPCBroker,
	c *grpc.ClientConn,
) (interface{}, error) {
	client := &infraClient{
		client:  pb.NewInfrastructureClient(c),
		logger:  p.Logger,
		broker:  broker,
		mappers: p.Mappers,
	}

	if p.ODR != nil && p.ODR.Enabled {
		client.odr = true
	}

	authenticator := &authenticatorClient{
		Client:  client.client,
		Logger:  client.logger,
		Broker:  client.broker,
		Mappers: client.mappers,
	}
	if ok, err := authenticator.Implements(ctx); err != nil {
		return nil, err
	} else if ok {
		p.Logger.Info("infra plugin capable of auth")
	} else {
		authenticator = nil
	}

	result := &mix_Infra_Authenticator{
		ConfigurableNotify: client,
		Infra:              client,
		Authenticator:      authenticator,
		Documented:         client,
	}
	return result, nil
}

// infraClient is an implementation of component.Infra that
// communicates over gRPC.
type infraClient struct {
	client  pb.InfrastructureClient
	logger  hclog.Logger
	broker  *plugin.GRPCBroker
	mappers []*argmapper.Func

	// indicates that the ODR version of the plugin should be used
	odr bool
}

func (c *infraClient) Config() (interface{}, error) {
	return configStructCall(context.Background(), c.client)
}

func (c *infraClient) ConfigSet(v interface{}) error {
	return configureCall(context.Background(), c.client, v)
}

func (c *infraClient) Documentation() (*docs.Documentation, error) {
	return documentationCall(context.Background(), c.client)
}

func (c *infraClient) InfraFunc() interface{} {
	if c.odr {
		c.logger.Debug("Running in ODR mode, attempting to retrieve ODR infra spec")

		// Get the infra spec
		spec, err := c.client.InfraSpecODR(context.Background(), &empty.Empty{})
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				// ok, this is an old plugin that doesn't support ODR mode, so just use
				// the basic mode.
				c.logger.Debug("plugin didn't implement InfraSpecODR, using Infra")
				goto basic
			}

			c.logger.Error("error retrieving ODR infra spec", "error", err)

			return funcErr(err)
		}

		// We don't want to be a mapper
		spec.Result = nil

		return funcspec.Func(spec, c.infraODR,
			argmapper.Logger(c.logger),
			argmapper.Typed(&pluginargs.Internal{
				Broker:  c.broker,
				Mappers: c.mappers,
				Cleanup: &pluginargs.Cleanup{},
			}),
		)
	} else {
		c.logger.Debug("Running in non-ODR mode, using Infra")
	}

basic:
	// Get the infra spec
	spec, err := c.client.InfraSpec(context.Background(), &empty.Empty{})
	if err != nil {
		return funcErr(err)
	}

	// We don't want to be a mapper
	spec.Result = nil

	return funcspec.Func(spec, c.infra,
		argmapper.Logger(c.logger),
		argmapper.Typed(&pluginargs.Internal{
			Broker:  c.broker,
			Mappers: c.mappers,
			Cleanup: &pluginargs.Cleanup{},
		}),
	)
}

func (c *infraClient) infra(
	ctx context.Context,
	args funcspec.Args,
) (component.Artifact, error) {
	// Call our function
	resp, err := c.client.Infra(ctx, &pb.FuncSpec_Args{Args: args})
	if err != nil {
		return nil, err
	}

	var tplData map[string]interface{}
	if len(resp.TemplateData) > 0 {
		if err := json.Unmarshal(resp.TemplateData, &tplData); err != nil {
			return nil, err
		}
	}

	return &plugincomponent.Artifact{
		Any:         resp.Result,
		AnyJson:     resp.ResultJson,
		LabelsVal:   resp.Labels,
		TemplateVal: tplData,
	}, nil
}

func (c *infraClient) infraODR(
	ctx context.Context,
	args funcspec.Args,
) (component.Artifact, error) {
	// Call our function
	resp, err := c.client.InfraODR(ctx, &pb.FuncSpec_Args{Args: args})
	if err != nil {
		return nil, err
	}

	var tplData map[string]interface{}
	if len(resp.TemplateData) > 0 {
		if err := json.Unmarshal(resp.TemplateData, &tplData); err != nil {
			return nil, err
		}
	}

	return &plugincomponent.Artifact{
		Any:         resp.Result,
		LabelsVal:   resp.Labels,
		TemplateVal: tplData,
	}, nil
}

// infraServer is a gRPC server that the client talks to and calls a
// real implementation of the component.
type infraServer struct {
	*base
	*authenticatorServer

	pb.UnsafeInfrastructureServer // to avoid having to copy stubs into here for authServer

	Impl component.Infra
}

func (s *infraServer) ConfigStruct(
	ctx context.Context,
	empty *empty.Empty,
) (*pb.Config_StructResp, error) {
	return configStruct(s.Impl)
}

func (s *infraServer) Configure(
	ctx context.Context,
	req *pb.Config_ConfigureRequest,
) (*empty.Empty, error) {
	return configure(s.Impl, req)
}

func (s *infraServer) Documentation(
	ctx context.Context,
	empty *empty.Empty,
) (*pb.Config_Documentation, error) {
	return documentation(s.Impl)
}

func (s *infraServer) InfraSpec(
	ctx context.Context,
	args *empty.Empty,
) (*pb.FuncSpec, error) {
	if s.Impl == nil {
		return nil, status.Errorf(codes.Unimplemented, "plugin does not implement: infra")
	}

	return funcspec.Spec(s.Impl.InfraFunc(),
		argmapper.Logger(s.Logger),
		argmapper.ConverterFunc(s.Mappers...),
		argmapper.Typed(s.internal()),
	)
}

func (s *infraServer) InfraSpecODR(
	ctx context.Context,
	args *empty.Empty,
) (*pb.FuncSpec, error) {
	if s.Impl == nil {
		return nil, status.Errorf(codes.Unimplemented, "plugin does not implement: infra")
	}

	odr, ok := s.Impl.(component.InfraODR)
	if !ok {
		return nil, status.Errorf(codes.Unimplemented, "plugin does not implement: infra")
	}

	return funcspec.Spec(odr.InfraODRFunc(),
		argmapper.Logger(s.Logger),
		argmapper.ConverterFunc(s.Mappers...),
		argmapper.Typed(s.internal()),
	)
}

func (s *infraServer) Infra(
	ctx context.Context,
	args *pb.FuncSpec_Args,
) (*pb.Infra_Resp, error) {
	internal := s.internal()
	defer internal.Cleanup.Close()

	encoded, encodedJson, raw, err := callDynamicFuncAny2(s.Impl.InfraFunc(), args.Args,
		argmapper.ConverterFunc(s.Mappers...),
		argmapper.Logger(s.Logger),
		argmapper.Typed(ctx),
		argmapper.Typed(internal),
	)
	if err != nil {
		return nil, err
	}

	result := &pb.Infra_Resp{Result: encoded, ResultJson: encodedJson}
	if artifact, ok := raw.(component.Artifact); ok {
		result.Labels = artifact.Labels()
	}

	result.TemplateData, err = templateData(raw)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *infraServer) InfraODR(
	ctx context.Context,
	args *pb.FuncSpec_Args,
) (*pb.Infra_Resp, error) {
	odr, ok := s.Impl.(component.InfraODR)
	if !ok {
		return nil, status.Errorf(codes.Unimplemented, "plugin does not implement: infra")
	}

	internal := s.internal()
	defer internal.Cleanup.Close()

	encoded, encodedJson, raw, err := callDynamicFuncAny2(odr.InfraODRFunc(), args.Args,
		argmapper.ConverterFunc(s.Mappers...),
		argmapper.Logger(s.Logger),
		argmapper.Typed(ctx),
		argmapper.Typed(internal),
	)
	if err != nil {
		return nil, err
	}

	result := &pb.Infra_Resp{Result: encoded, ResultJson: encodedJson}
	if artifact, ok := raw.(component.Artifact); ok {
		result.Labels = artifact.Labels()
	}

	result.TemplateData, err = templateData(raw)
	if err != nil {
		return nil, err
	}

	return result, nil
}

var (
	_ plugin.Plugin                = (*InfraPlugin)(nil)
	_ plugin.GRPCPlugin            = (*InfraPlugin)(nil)
	_ pb.InfrastructureServer      = (*infraServer)(nil)
	_ component.Infra              = (*infraClient)(nil)
	_ component.Configurable       = (*infraClient)(nil)
	_ component.Documented         = (*infraClient)(nil)
	_ component.ConfigurableNotify = (*infraClient)(nil)
)
