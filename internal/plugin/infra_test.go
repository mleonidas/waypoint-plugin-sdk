package plugin

import (
	"context"
	"testing"

	"github.com/hashicorp/go-argmapper"
	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/opaqueany"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/waypoint-plugin-sdk/component"
	"github.com/hashicorp/waypoint-plugin-sdk/component/mocks"
	"github.com/hashicorp/waypoint-plugin-sdk/internal/testproto"
	pb "github.com/hashicorp/waypoint-plugin-sdk/proto/gen"
)

func TestInfraStructureInfra(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	called := false
	infraFunc := func(ctx context.Context, args *component.Source) *testproto.Data {
		called = true
		assert.NotNil(ctx)
		assert.Equal("foo", args.App)
		return &testproto.Data{Value: "hello"}
	}

	mockB := &mocks.Infra{}
	mockB.On("InfraFunc").Return(infraFunc)

	plugins := Plugins(WithComponents(mockB), WithMappers(testDefaultMappers(t)...))
	client, server := plugin.TestPluginGRPCConn(t, plugins[1])

	defer client.Close()
	defer server.Stop()

	raw, err := client.Dispense("infra")
	require.NoError(err)
	infra := raw.(component.Infra)
	f := infra.InfraFunc().(*argmapper.Func)
	require.NotNil(f)

	result := f.Call(
		argmapper.Typed(context.Background()),
		argmapper.Typed(&pb.Args_Source{App: "foo"}),
	)
	require.NoError(result.Err())

	raw = result.Out(0)
	require.NotNil(raw)
	require.Implements((*component.Artifact)(nil), raw)

	anyVal := raw.(component.ProtoMarshaler).Proto().(*opaqueany.Any)
	name := anyVal.MessageName()
	require.NoError(err)
	require.Equal("testproto.Data", string(name))

	require.True(called)
}

func TestInfraDynamicFunc_auth(t *testing.T) {
	testDynamicFunc(t, "infra", &mockInfraAuthenticator{}, func(v, f interface{}) {
		v.(*mockInfraAuthenticator).Authenticator.On("AuthFunc").Return(f)
	}, func(raw interface{}) interface{} {
		return raw.(component.Authenticator).AuthFunc()
	})
}

func TestInfraDynamicFunc_validateAuth(t *testing.T) {
	testDynamicFunc(t, "infra", &mockInfraAuthenticator{}, func(v, f interface{}) {
		v.(*mockInfraAuthenticator).Authenticator.On("ValidateAuthFunc").Return(f)
	}, func(raw interface{}) interface{} {
		return raw.(component.Authenticator).ValidateAuthFunc()
	})
}

func TestInfraConfig(t *testing.T) {
	mockV := &mockInfraConfigurable{}
	testConfigurable(t, "infra", mockV, &mockV.Configurable)
}

type mockInfraAuthenticator struct {
	mocks.Infra
	mocks.Authenticator
}

type mockInfraConfigurable struct {
	mocks.Infra
	mocks.Configurable
}
