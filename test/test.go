/*
 * Copyright 2018 The Service Manager Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Peripli/service-manager/pkg/query"
	"github.com/Peripli/service-manager/pkg/types"
	"github.com/gavv/httpexpect"
	"time"

	"github.com/tidwall/gjson"

	"github.com/Peripli/service-manager/pkg/multitenancy"

	"github.com/Peripli/service-manager/pkg/web"

	"github.com/Peripli/service-manager/pkg/env"
	"github.com/Peripli/service-manager/pkg/sm"

	. "github.com/onsi/gomega"

	"github.com/Peripli/service-manager/test/common"
	. "github.com/onsi/ginkgo"
)

type Op string

type ResponseMode bool

const (
	Get        Op = "get"
	List       Op = "list"
	Delete     Op = "delete"
	DeleteList Op = "deletelist"
	Patch      Op = "patch"

	Sync  ResponseMode = false
	Async ResponseMode = true
)

type MultitenancySettings struct {
	ClientID           string
	ClientIDTokenClaim string
	TenantTokenClaim   string
	LabelKey           string

	TokenClaims map[string]interface{}
}

type TestCase struct {
	API                     string
	SupportsAsyncOperations bool
	SupportedOps            []Op
	ResourceType            types.ObjectType

	MultitenancySettings   *MultitenancySettings
	DisableTenantResources bool

	ResourceBlueprint                      func(ctx *common.TestContext, smClient *common.SMExpect, async bool) common.Object
	ResourceWithoutNullableFieldsBlueprint func(ctx *common.TestContext, smClient *common.SMExpect, async bool) common.Object
	PatchResource                          func(ctx *common.TestContext, apiPath string, objID string, resourceType types.ObjectType, patchLabels []*query.LabelChange, async bool)

	AdditionalTests func(ctx *common.TestContext)
}

func DefaultResourcePatch(ctx *common.TestContext, apiPath string, objID string, _ types.ObjectType, patchLabels []*query.LabelChange, async bool) {
	patchLabelsBody := make(map[string]interface{})
	patchLabelsBody["labels"] = patchLabels

	By(fmt.Sprintf("Attempting to patch resource of %s with labels as labels are declared supported", apiPath))
	resp := ctx.SMWithOAuth.PATCH(apiPath+"/"+objID).WithQuery("async", strconv.FormatBool(async)).WithJSON(patchLabelsBody).Expect()

	if async {
		resp = resp.Status(http.StatusAccepted)
		err := ExpectOperation(ctx.SMWithOAuth, resp, types.SUCCEEDED)
		if err != nil {
			panic(err)
		}
	} else {
		resp.Status(http.StatusOK)
	}

}

func ExpectOperation(auth *common.SMExpect, asyncResp *httpexpect.Response, expectedState types.OperationState) error {
	return ExpectOperationWithError(auth, asyncResp, expectedState, "")
}

func ExpectOperationWithError(auth *common.SMExpect, asyncResp *httpexpect.Response, expectedState types.OperationState, expectedErrMsg string) error {
	operationURL := asyncResp.Header("Location").Raw()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := fmt.Errorf("unable to verify operation state (expected state = %s)", string(expectedState))
	var operation *httpexpect.Object
	for {
		select {
		case <-ctx.Done():
		default:
			operation = auth.GET(operationURL).
				Expect().Status(http.StatusOK).JSON().Object()
			state := operation.Value("state").String().Raw()
			if state == string(expectedState) {
				errs := operation.Value("errors")
				if expectedState == types.SUCCEEDED {
					errs.Null()
				} else {
					errs.NotNull()
					errMsg := errs.Object().Value("message").String().Raw()

					if !strings.Contains(errMsg, expectedErrMsg) {
						err = fmt.Errorf("unable to verify operation - expected error message (%s), but got (%s)", expectedErrMsg, errs.String().Raw())
					}
				}
				return nil
			}
		}
	}
	return err
}

func DescribeTestsFor(t TestCase) bool {
	return Describe(t.API, func() {
		var ctx *common.TestContext

		AfterSuite(func() {
			ctx.Cleanup()
		})

		func() {
			By("==== Preparation for SM tests... ====")

			defer GinkgoRecover()
			ctxBuilder := common.NewTestContextBuilderWithSecurity()

			if t.MultitenancySettings != nil {
				ctxBuilder.
					WithTenantTokenClaims(t.MultitenancySettings.TokenClaims).
					WithSMExtensions(func(ctx context.Context, smb *sm.ServiceManagerBuilder, e env.Environment) error {
						smb.EnableMultitenancy(t.MultitenancySettings.LabelKey, func(request *web.Request) (string, error) {
							extractTenantFromToken := multitenancy.ExtractTenantFromTokenWrapperFunc(t.MultitenancySettings.TenantTokenClaim)
							user, ok := web.UserFromContext(request.Context())
							if !ok {
								return "", nil
							}
							var userData json.RawMessage
							if err := user.Data(&userData); err != nil {
								return "", fmt.Errorf("could not unmarshal claims from token: %s", err)
							}
							clientIDFromToken := gjson.GetBytes([]byte(userData), t.MultitenancySettings.ClientIDTokenClaim).String()
							if t.MultitenancySettings.ClientID != clientIDFromToken {
								return "", nil
							}
							user.AccessLevel = web.TenantAccess
							request.Request = request.WithContext(web.ContextWithUser(request.Context(), user))
							return extractTenantFromToken(request)
						})
						return nil
					})
			}
			ctx = ctxBuilder.Build()

			// A panic outside of Ginkgo's primitives (during test setup) would be recovered
			// by the deferred GinkgoRecover() and the error will be associated with the first
			// It to be ran in the suite. There, we add a dummy It to reduce confusion.
			It("sets up all test prerequisites that are ran outside of Ginkgo primitives properly", func() {
				Expect(true).To(BeTrue())
			})

			responseModes := []ResponseMode{Sync}
			if t.SupportsAsyncOperations {
				responseModes = append(responseModes, Async)
			}

			for _, op := range t.SupportedOps {
				for _, respMode := range responseModes {
					switch op {
					case Get:
						DescribeGetTestsfor(ctx, t, respMode)
					case List:
						DescribeListTestsFor(ctx, t, respMode)
					case Delete:
						DescribeDeleteTestsfor(ctx, t, respMode)
					case DeleteList:
						if respMode == Sync {
							DescribeDeleteListFor(ctx, t)
						}
					case Patch:
						DescribePatchTestsFor(ctx, t, respMode)
					default:
						_, err := fmt.Fprintf(GinkgoWriter, "Generic test cases for op %s are not implemented\n", op)
						if err != nil {
							panic(err)
						}
					}
				}
			}

			if t.AdditionalTests != nil {
				t.AdditionalTests(ctx)
			}

			By("==== Successfully finished preparation for SM tests. Running API tests suite... ====")
		}()
	})
}
