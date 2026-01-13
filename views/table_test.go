package views

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

var _ = Describe("View Database Table", func() {
	testdataDir := "testdata/auto"
	files, err := os.ReadDir(testdataDir)
	Expect(err).ToNot(HaveOccurred())

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}

		It(file.Name(), func() {
			viewObj, err := loadViewFromYAML(filepath.Join("auto", file.Name()))
			Expect(err).ToNot(HaveOccurred())

			err = db.PersistViewFromCRD(DefaultContext, viewObj)
			Expect(err).ToNot(HaveOccurred())

			// save the results to DB first so ReadOrPopulateViewTable reads them
			request := &requestOpt{includeRows: true}
			_, err = populateView(DefaultContext, viewObj, request)
			Expect(err).ToNot(HaveOccurred())

			// Verify that last_refresh field is populated after PopulateView
			lastRefresh, err := getLastRefresh(DefaultContext, string(viewObj.GetUID()), request.Fingerprint())
			Expect(err).ToNot(HaveOccurred())
			Expect(lastRefresh).ToNot(BeNil())

			tableName := viewObj.TableName()
			Expect(DefaultContext.DB().Migrator().HasTable(tableName)).To(BeTrue())

			result, err := ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), viewObj.Namespace, viewObj.Name, WithIncludeRows(true))
			Expect(err).ToNot(HaveOccurred())

			expectedRowsAnnotation, exists := viewObj.Annotations["expected-rows"]
			Expect(exists).To(BeTrue(), "expected-rows annotation not found for view: %s", viewObj.Name)

			var expectedRows [][]any
			err = json.Unmarshal([]byte(expectedRowsAnnotation), &expectedRows)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Rows).To(HaveLen(len(expectedRows)))

			expectedResults := make([]pkgView.Row, len(expectedRows))
			for i, row := range expectedRows {
				expectedResults[i] = pkgView.Row(row)
			}
			Expect(result.Rows).To(Equal(expectedResults))

			expectedPanelsAnnotation, exists := viewObj.Annotations["expected-panels"]
			if exists {
				var expectedPanels []api.PanelResult
				err = json.Unmarshal([]byte(expectedPanelsAnnotation), &expectedPanels)
				Expect(err).ToNot(HaveOccurred())

				Expect(result.Panels).To(HaveLen(len(expectedPanels)))

				// Convert both to JSON and back to normalize types (int64 vs float64)
				actualJSON, err := json.Marshal(result.Panels)
				Expect(err).ToNot(HaveOccurred())

				expectedJSON, err := json.Marshal(expectedPanels)
				Expect(err).ToNot(HaveOccurred())

				Expect(string(actualJSON)).To(Equal(string(expectedJSON)))
			}
		})
	}
})

var _ = Describe("ReadOrPopulateViewTable", func() {
	Describe("Cache Control", func() {
		It("should return an error when refresh-timeout is very low", func() {
			viewObj, err := loadViewFromYAML("cache-test-view.yaml")
			Expect(err).ToNot(HaveOccurred())

			err = db.PersistViewFromCRD(DefaultContext, viewObj)
			Expect(err).ToNot(HaveOccurred())

			tableName := viewObj.TableName()
			if DefaultContext.DB().Migrator().HasTable(tableName) {
				err = DefaultContext.DB().Migrator().DropTable(tableName)
				Expect(err).ToNot(HaveOccurred())
			}

			_, err = ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), viewObj.Namespace, viewObj.Name, WithRefreshTimeout(1*time.Microsecond))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("refresh timeout reached. try again"))
		})

		It("should return cached data with refresh error status when refresh fails", func() {
			viewObj, err := loadViewFromYAML("cache-test-view.yaml")
			Expect(err).ToNot(HaveOccurred())

			viewObj.Spec.Cache.MaxAge = "0s"

			err = db.PersistViewFromCRD(DefaultContext, viewObj)
			Expect(err).ToNot(HaveOccurred())

			result, err := ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), viewObj.Namespace, viewObj.Name, WithIncludeRows(true))
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RefreshStatus).To(Equal(api.ViewRefreshStatusFresh))
			Expect(result.RefreshError).To(BeEmpty())
			Expect(result.ResponseSource).To(Equal(api.ViewResponseSourceFresh))

			time.Sleep(2 * time.Millisecond)
			viewObj.Spec.Queries["pod"].Configs.Search = "name >= test"
			err = db.PersistViewFromCRD(DefaultContext, viewObj)
			Expect(err).ToNot(HaveOccurred())

			cachedResult, err := ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), viewObj.Namespace, viewObj.Name, WithIncludeRows(true))
			Expect(err).ToNot(HaveOccurred())
			Expect(cachedResult.RefreshStatus).To(Equal(api.ViewRefreshStatusError))
			Expect(cachedResult.RefreshError).ToNot(BeEmpty())
			Expect(cachedResult.ResponseSource).To(Equal(api.ViewResponseSourceCache))
		})
	})

	Describe("Request variables", Ordered, func() {
		var namespaceView, namespaceWithDefaultTemplateVars *v1.View

		BeforeAll(func() {
			var err error
			namespaceView, err = loadViewFromYAML("namespace.yaml")
			Expect(err).ToNot(HaveOccurred())

			err = db.PersistViewFromCRD(DefaultContext, namespaceView)
			Expect(err).ToNot(HaveOccurred())

			namespaceWithDefaultTemplateVars, err = loadViewFromYAML("namespace-with-defaults.yaml")
			Expect(err).ToNot(HaveOccurred())

			err = db.PersistViewFromCRD(DefaultContext, namespaceWithDefaultTemplateVars)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should populate variables with default options", func() {
			result, err := ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), namespaceWithDefaultTemplateVars.Namespace, namespaceWithDefaultTemplateVars.Name, WithIncludeRows(false))
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Variables).To(HaveLen(2))

			variableWithOptions := lo.Map(namespaceWithDefaultTemplateVars.Spec.Templating, func(v api.ViewVariable, _ int) api.ViewVariableWithOptions {
				vo := api.ViewVariableWithOptions{
					ViewVariable: v,
				}

				switch v.Key {
				case "cluster":
					vo.ViewVariable.Default = "demo"
					vo.Options = []string{"demo"}
					vo.OptionItems = optionItemsFromValues(vo.Options)
				case "namespace":
					vo.ViewVariable.Default = "dummy-namespace"
					vo.Options = []string{"flux", "missioncontrol"}
					vo.OptionItems = optionItemsFromValues(vo.Options)
				}

				return vo
			})

			Expect(result.Variables).To(Equal(variableWithOptions))
		})

		It("should populate variables when not provided in request", func() {
			result, err := ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), namespaceView.Namespace, namespaceView.Name, WithIncludeRows(false))
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Variables).To(HaveLen(2))

			variableWithOptions := lo.Map(namespaceView.Spec.Templating, func(v api.ViewVariable, _ int) api.ViewVariableWithOptions {
				vo := api.ViewVariableWithOptions{
					ViewVariable: v,
				}

				switch v.Key {
				case "cluster":
					vo.ViewVariable.Default = "demo"
					vo.Options = []string{"demo"}
					vo.OptionItems = optionItemsFromValues(vo.Options)
				case "namespace":
					vo.ViewVariable.Default = "flux"
					vo.Options = []string{"flux", "missioncontrol"}
					vo.OptionItems = optionItemsFromValues(vo.Options)
				}

				return vo
			})

			Expect(result.Variables).To(Equal(variableWithOptions))
		})

		It("should evaluate CEL templates for config-derived variable options", func() {
			variable := api.ViewVariable{
				Key:   "release",
				Label: "Release",
				ValueFrom: &api.ViewVariableValueFrom{
					LabelTemplate: "config.name",
					ValueTemplate: "config.id",
					Config: types.ResourceSelector{
						Types: []string{"Helm::Release"},
					},
				},
			}

			variables, _, err := populateViewVariables(DefaultContext, []api.ViewVariable{variable}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(variables).To(HaveLen(1))

			expectedItems := []api.ViewVariableOption{
				{Label: lo.FromPtr(dummy.NginxHelmRelease.Name), Value: dummy.NginxHelmRelease.ID.String()},
				{Label: lo.FromPtr(dummy.RedisHelmRelease.Name), Value: dummy.RedisHelmRelease.ID.String()},
			}

			Expect(variables[0].OptionItems).To(ContainElements(expectedItems))
		})
	})
})

// testViewRequestHelper is a helper function to test view requests with variables
func testViewRequestHelper(ctx context.Context, viewObj *v1.View, variables map[string]string, expectedRows int) *api.ViewResult {
	return testViewRequestHelperWithMaxAge(ctx, viewObj, variables, expectedRows, 0)
}

// testViewRequestHelperWithMaxAge is a helper function with configurable cache max age
func testViewRequestHelperWithMaxAge(ctx context.Context, viewObj *v1.View, variables map[string]string, expectedRows int, maxAge time.Duration) *api.ViewResult {
	request := &requestOpt{
		includeRows: true,
		variables:   variables,
	}

	opts := []ViewOption{}
	for k, v := range variables {
		opts = append(opts, WithVariable(k, v))
	}
	opts = append(opts, WithIncludeRows(true))
	if maxAge > 0 {
		opts = append(opts, WithMaxAge(maxAge))
	}

	result, err := ReadOrPopulateViewTable(ctx, viewObj.GetNamespace(), viewObj.GetName(), opts...)
	Expect(err).ToNot(HaveOccurred())
	Expect(result.Rows).To(HaveLen(expectedRows))

	lastRefresh, err := getLastRefresh(ctx, string(viewObj.GetUID()), request.Fingerprint())
	Expect(err).ToNot(HaveOccurred())
	Expect(lastRefresh).ToNot(BeNil())

	return result
}

var _ = Describe("View Variables Caching", func() {
	It("should cache results separately for different namespace variables", func() {
		// Create a test view with namespace variable
		viewObj := &v1.View{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-view-test",
				Namespace: "default",
				UID:       "a0580b13-61ad-47c2-b02a-1cac446be75b",
			},
			Spec: v1.ViewSpec{
				Columns: []pkgView.ColumnDef{
					{Name: "name", Type: pkgView.ColumnTypeString, PrimaryKey: true},
					{Name: "namespace", Type: pkgView.ColumnTypeString, PrimaryKey: true},
					{Name: "status", Type: pkgView.ColumnTypeString},
				},
				Queries: map[string]v1.ViewQueryWithColumnDefs{
					"pods": {
						Query: pkgView.Query{
							Configs: &types.ResourceSelector{
								TagSelector: "namespace=$(var.namespace)",
								Types:       []string{"Kubernetes::Pod"},
							},
						},
					},
				},
				Mapping: map[string]types.CelExpression{
					"namespace": "row.tags.namespace",
				},
				Templating: []api.ViewVariable{
					{
						Key:    "namespace",
						Label:  "Namespace",
						Values: []string{"missioncontrol", "ingress-nginx"},
					},
				},
			},
		}

		err := db.PersistViewFromCRD(DefaultContext, viewObj)
		Expect(err).ToNot(HaveOccurred())

		testViewRequestHelper(DefaultContext, viewObj, map[string]string{"namespace": "missioncontrol"}, 2)
		testViewRequestHelper(DefaultContext, viewObj, map[string]string{"namespace": "ingress-nginx"}, 1)
	})

	It("should use cached results when same variables are provided", func() {
		viewObj := &v1.View{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cache-test-pod-view",
				Namespace: "default",
				UID:       "022bce1e-8274-438e-8294-fdb8ec25cdd2",
			},
			Spec: v1.ViewSpec{
				Columns: []pkgView.ColumnDef{
					{Name: "name", Type: pkgView.ColumnTypeString, PrimaryKey: true},
					{Name: "namespace", Type: pkgView.ColumnTypeString, PrimaryKey: true},
				},
				Queries: map[string]v1.ViewQueryWithColumnDefs{
					"pods": {
						Query: pkgView.Query{
							Configs: &types.ResourceSelector{
								TagSelector: "namespace=$(var.namespace)",
								Types:       []string{"Kubernetes::Pod"},
							},
						},
					},
				},
				Mapping: map[string]types.CelExpression{
					"namespace": "row.tags.namespace",
				},
				Templating: []api.ViewVariable{
					{
						Key:    "namespace",
						Label:  "Namespace",
						Values: []string{"missioncontrol"},
					},
				},
			},
		}

		err := db.PersistViewFromCRD(DefaultContext, viewObj)
		Expect(err).ToNot(HaveOccurred())

		// The first request returns the result directly. Ensure that it returns the last refreshedAt time.
		firstResult := testViewRequestHelper(DefaultContext, viewObj, map[string]string{"namespace": "missioncontrol"}, 2)
		Expect(firstResult.LastRefreshedAt).To(BeTemporally("~", time.Now(), 5*time.Second))

		// Multiple sequential requests. and ensure they all come from the cache.
		secondResult := testViewRequestHelper(DefaultContext, viewObj, map[string]string{"namespace": "missioncontrol"}, 2)
		thirdResult := testViewRequestHelper(DefaultContext, viewObj, map[string]string{"namespace": "missioncontrol"}, 2)
		fourthResult := testViewRequestHelper(DefaultContext, viewObj, map[string]string{"namespace": "missioncontrol"}, 2)
		Expect(secondResult.LastRefreshedAt).To(Equal(thirdResult.LastRefreshedAt))
		Expect(thirdResult.LastRefreshedAt).To(Equal(fourthResult.LastRefreshedAt))
	})

	It("should handle requests without variables", func() {
		viewObj := &v1.View{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "no-vars-view",
				Namespace: "default",
				UID:       "77c1d1c5-1e2e-4dd2-92c6-e2791994c272",
			},
			Spec: v1.ViewSpec{
				Columns: []pkgView.ColumnDef{
					{Name: "name", Type: pkgView.ColumnTypeString, PrimaryKey: true},
				},
				Queries: map[string]v1.ViewQueryWithColumnDefs{
					"data": {
						Query: pkgView.Query{
							Configs: &types.ResourceSelector{
								TagSelector: "namespace=ingress-nginx",
								Types:       []string{"Kubernetes::Pod"},
							},
						},
					},
				},
			},
		}

		err := db.PersistViewFromCRD(DefaultContext, viewObj)
		Expect(err).ToNot(HaveOccurred())

		testViewRequestHelper(DefaultContext, viewObj, map[string]string{}, 1)
		testViewRequestHelper(DefaultContext, viewObj, map[string]string{}, 1)
	})
})

// loadViewFromYAML loads a View from a YAML fixture file
func loadViewFromYAML(filename string) (*v1.View, error) {
	yamlPath := filepath.Join("testdata", filename)
	yamlData, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	var view v1.View
	err = yaml.Unmarshal(yamlData, &view)
	if err != nil {
		return nil, err
	}

	return &view, nil
}
