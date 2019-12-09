package sm

import (
	"context"
	"fmt"
	"strings"

	"github.com/Peripli/service-manager/pkg/security/filters/middlewares"
	"github.com/Peripli/service-manager/pkg/security/http"
	"github.com/Peripli/service-manager/pkg/security/http/authz"

	"github.com/Peripli/service-manager/pkg/log"
	httpsec "github.com/Peripli/service-manager/pkg/security/http"
	"github.com/Peripli/service-manager/pkg/types"
	"github.com/Peripli/service-manager/pkg/util/slice"
	"github.com/Peripli/service-manager/pkg/web"
)

var TypeToPath = map[types.ObjectType]string{
	types.ServiceBrokerType:   web.ServiceBrokersURL + "/**",
	types.PlatformType:        web.PlatformsURL + "/**",
	types.ServiceOfferingType: web.ServiceOfferingsURL + "/**",
	types.ServicePlanType:     web.ServicePlansURL + "/**",
	types.VisibilityType:      web.VisibilitiesURL + "/**",
	types.NotificationType:    web.NotificationsURL + "/**",
	types.ServiceInstanceType: web.ServiceInstancesURL + "/**",
}

type authorizerBuilder struct {
	parent *authorizerBuilder

	objectType types.ObjectType
	path       string
	methods    []string

	authorizers []httpsec.Authorizer

	cloneSpace            string
	clientID              string
	trustedClientIDSuffix string
	attachFunc            func(web.Filter)
	done                  func() *ServiceManagerBuilder
}

func (ab *authorizerBuilder) Configure(cloneSpace, clientID, trustedClientIDSuffix string) *authorizerBuilder {
	ab.cloneSpace = cloneSpace
	ab.clientID = clientID
	ab.trustedClientIDSuffix = trustedClientIDSuffix
	return ab
}

func (ab *authorizerBuilder) Custom(authorizer httpsec.Authorizer) *authorizerBuilder {
	ab.authorizers = append(ab.authorizers, authorizer)
	return ab
}

func (ab *authorizerBuilder) Global(scopes ...string) *authorizerBuilder {
	ab.authorizers = append(ab.authorizers, authz.NewAndAuthorizer(
		NewOAuthCloneAuthorizer(ab.trustedClientIDSuffix, web.GlobalAccess),
		NewRequiredScopesAuthorizer(PrefixScopes(ab.cloneSpace, scopes...), web.GlobalAccess),
	))
	return ab
}

func (ab *authorizerBuilder) Tenant(tenantScopes ...string) *authorizerBuilder {
	ab.authorizers = append(ab.authorizers, authz.NewAndAuthorizer(
		authz.NewOrAuthorizer(
			NewOauthClientAuthorizer(ab.clientID, web.GlobalAccess),
			NewOAuthCloneAuthorizer(ab.trustedClientIDSuffix, web.GlobalAccess),
		),
		NewRequiredScopesAuthorizer(PrefixScopes(ab.cloneSpace, tenantScopes...), web.TenantAccess),
	))
	return ab
}

func (ab *authorizerBuilder) AllTenant(allTenantScopes ...string) *authorizerBuilder {
	ab.authorizers = append(ab.authorizers, authz.NewAndAuthorizer(
		NewOAuthCloneAuthorizer(ab.trustedClientIDSuffix, web.GlobalAccess),
		NewRequiredScopesAuthorizer(PrefixScopes(ab.cloneSpace, allTenantScopes...), web.AllTenantAccess),
	))
	return ab
}

func (ab *authorizerBuilder) For(methods ...string) *authorizerBuilder {
	ab.methods = methods
	return ab
}

func (ab *authorizerBuilder) And() *authorizerBuilder {
	return &authorizerBuilder{
		parent:                ab,
		path:                  ab.path,
		objectType:            ab.objectType,
		cloneSpace:            ab.cloneSpace,
		clientID:              ab.clientID,
		trustedClientIDSuffix: ab.trustedClientIDSuffix,
		attachFunc:            ab.attachFunc,
		done:                  ab.done,
	}
}

func (ab *authorizerBuilder) Register() *ServiceManagerBuilder {
	current := ab
	for current != nil {
		path := current.path
		if len(path) == 0 {
			path = TypeToPath[current.objectType]
		}
		if len(current.methods) == 0 {
			log.D().Panicf("Cannot register authorization for %s with no methods", path)
		}
		filter := NewAuthzFilter(current.methods, path, authz.NewOrAuthorizer(current.authorizers...))
		current.attachFunc(filter)
		current = current.parent
	}
	return ab.done()
}

// NewOAuthCloneAuthorizer returns OAuth authorizer
func NewOAuthCloneAuthorizer(trustedClientIDSuffix string, level web.AccessLevel) httpsec.Authorizer {
	return newBaseAuthorizer(func(ctx context.Context, userContext *web.UserContext) (httpsec.Decision, web.AccessLevel, error) {
		var claims struct {
			ZID string
			CID string
		}
		logger := log.C(ctx)
		if err := userContext.Data(&claims); err != nil {
			return httpsec.Deny, web.NoAccess, fmt.Errorf("invalid token: %v", err)
		}
		logger.Debugf("User token: zid=%s cid=%s", claims.ZID, claims.CID)

		if !slice.StringsAnySuffix([]string{claims.CID}, trustedClientIDSuffix) {
			logger.Debugf(`Client id "%s" from user token is not generated from a clone OAuth client %v`, claims.CID, trustedClientIDSuffix)
			return httpsec.Deny, web.NoAccess, fmt.Errorf(`client id "%s" from user token is not generated from a clone OAuth client`, claims.CID)
		}

		return httpsec.Allow, level, nil
	})
}

// NewRequiredScopesAuthorizer returns OAuth authorizer which denys if scopes not presented
func NewRequiredScopesAuthorizer(requiredScopes []string, level web.AccessLevel) httpsec.Authorizer {
	return newScopesAuthorizer(requiredScopes, true, level)
}

// NewOptionalScopesAuthorizer returns OAuth authorizer which abstains if scopes not presented
func NewOptionalScopesAuthorizer(optionalScopes []string, level web.AccessLevel) httpsec.Authorizer {
	return newScopesAuthorizer(optionalScopes, false, level)
}

func newScopesAuthorizer(scopes []string, mandatory bool, level web.AccessLevel) httpsec.Authorizer {
	return newBaseAuthorizer(func(ctx context.Context, userContext *web.UserContext) (httpsec.Decision, web.AccessLevel, error) {
		var claims struct {
			Scopes []string `json:"scope"`
		}
		if err := userContext.Data(&claims); err != nil {
			return httpsec.Deny, web.NoAccess, fmt.Errorf("could not extract scopes from token: %v", err)
		}
		userScopes := claims.Scopes
		log.C(ctx).Debugf("User token scopes: %v", userScopes)

		for _, scope := range scopes {
			if slice.StringsAnyEquals(userScopes, scope) {
				return httpsec.Allow, level, nil
			}
		}
		if mandatory {
			return httpsec.Deny, web.NoAccess, fmt.Errorf(`none of the required scopes %v are present in the user token scopes %v`,
				scopes, userScopes)
		}

		log.C(ctx).Debugf("none of the optional scopes %v are present in the user token scopes %v", scopes, userScopes)
		return httpsec.Abstain, web.NoAccess, nil
	})
}

func PrefixScopes(space string, scopes ...string) []string {
	prefixedScopes := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		prefixedScopes = append(prefixedScopes, prefixScope(space, scope))
	}
	return prefixedScopes
}

func prefixScope(space, scope string) string {
	return fmt.Sprintf("%s.%s", space, scope)
}

type baseAuthorizer struct {
	userProcessingFunc func(context.Context, *web.UserContext) (httpsec.Decision, web.AccessLevel, error)
}

func newBaseAuthorizer(userProcessingFunc func(context.Context, *web.UserContext) (httpsec.Decision, web.AccessLevel, error)) *baseAuthorizer {
	return &baseAuthorizer{userProcessingFunc: userProcessingFunc}
}

func (a *baseAuthorizer) Authorize(request *web.Request) (httpsec.Decision, web.AccessLevel, error) {
	ctx := request.Context()

	user, ok := web.UserFromContext(ctx)
	if !ok {
		return httpsec.Abstain, web.NoAccess, nil
	}

	if user.AuthenticationType != web.Bearer {
		return httpsec.Abstain, web.NoAccess, nil // Not oauth
	}

	decision, accessLevel, err := a.userProcessingFunc(ctx, user)
	if err != nil {
		// denying with an error is allowed so in case of an error we return the decision as well
		return decision, accessLevel, err
	}

	request.Request = request.WithContext(web.ContextWithUser(ctx, user))

	return decision, accessLevel, nil
}

func NewOauthClientAuthorizer(clientID string, level web.AccessLevel) http.Authorizer {
	return newBaseAuthorizer(func(ctx context.Context, userContext *web.UserContext) (http.Decision, web.AccessLevel, error) {
		var cid struct {
			CID string
		}
		logger := log.C(ctx)
		if err := userContext.Data(&cid); err != nil {
			return http.Deny, web.NoAccess, fmt.Errorf("invalid token: %v", err)
		}
		logger.Debugf("User token cid=%s", cid.CID)
		if cid.CID != clientID {
			logger.Debugf(`Client id "%s" from user token does not match the required client-id %s`, cid.CID, clientID)
			return http.Deny, web.NoAccess, fmt.Errorf(`client id "%s" from user token is not trusted`, cid.CID)
		}
		return http.Allow, level, nil
	})
}

// NewAuthzFilter returns a web.Filter for a specific scope and endpoint
func NewAuthzFilter(methods []string, path string, authorizer http.Authorizer) *AuthorizationFilter {
	filterName := fmt.Sprintf("%s-AuthzFilter@%s", strings.Join(methods, "/"), path)
	return &AuthorizationFilter{
		Authorization: &middlewares.Authorization{
			Authorizer: authorizer,
		},
		methods: methods,
		path:    path,
		name:    filterName,
	}
}

type AuthorizationFilter struct {
	*middlewares.Authorization

	methods []string
	path    string
	name    string
}

func (af *AuthorizationFilter) Name() string {
	return af.name
}

// FilterMatchers implements the web.Filter interface and returns the conditions
// on which the filter should be executed
func (af *AuthorizationFilter) FilterMatchers() []web.FilterMatcher {
	return []web.FilterMatcher{
		{
			Matchers: []web.Matcher{
				web.Methods(af.methods...),
				web.Path(af.path),
			},
		},
	}
}
