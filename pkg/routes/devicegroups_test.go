// FIXME: golangci-lint
// nolint:errcheck,gosec,govet,ineffassign,revive,staticcheck,typecheck
package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/bxcodec/faker/v3"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/redhatinsights/edge-api/config"
	"github.com/redhatinsights/edge-api/internal/testing"
	"github.com/redhatinsights/edge-api/pkg/db"
	apiErrors "github.com/redhatinsights/edge-api/pkg/errors"
	"github.com/redhatinsights/edge-api/pkg/routes/common"
	feature "github.com/redhatinsights/edge-api/unleash/features"

	"github.com/redhatinsights/edge-api/pkg/services"

	"github.com/golang/mock/gomock"
	"github.com/redhatinsights/edge-api/pkg/models"
	"github.com/redhatinsights/edge-api/pkg/services/mock_services"

	"github.com/go-chi/chi/v5"
	"github.com/redhatinsights/edge-api/pkg/dependencies"
	log "github.com/sirupsen/logrus"
)

var _ = Describe("DeviceGroup routes", func() {
	var (
		ctrl                    *gomock.Controller
		mockDeviceGroupsService *mock_services.MockDeviceGroupsServiceInterface
		mockUpdateService       *mock_services.MockUpdateServiceInterface
		mockCommitService       *mock_services.MockCommitServiceInterface
		mockDeviceService       *mock_services.MockDeviceServiceInterface
		edgeAPIServices         *dependencies.EdgeAPIServices
		deviceGroupName         = "test-device-group"
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockDeviceGroupsService = mock_services.NewMockDeviceGroupsServiceInterface(ctrl)
		mockUpdateService = mock_services.NewMockUpdateServiceInterface(ctrl)
		mockCommitService = mock_services.NewMockCommitServiceInterface(ctrl)
		mockDeviceService = mock_services.NewMockDeviceServiceInterface(ctrl)
		edgeAPIServices = &dependencies.EdgeAPIServices{
			DeviceGroupsService: mockDeviceGroupsService,
			UpdateService:       mockUpdateService,
			DeviceService:       mockDeviceService,
			CommitService:       mockCommitService,
			Log:                 log.NewEntry(log.StandardLogger()),
		}
		Expect(ctrl).ToNot(BeNil())
		Expect(mockDeviceGroupsService).ToNot(BeNil())
		Expect(edgeAPIServices).ToNot(BeNil())
	})
	AfterEach(func() {
		ctrl.Finish()
	})
	Context("get all device-groups with filter parameters", func() {
		tt := []struct {
			name          string
			params        string
			expectedError []validationError
		}{
			{
				name:   "bad created_at date",
				params: "created_at=today",
				expectedError: []validationError{
					{Key: "created_at", Reason: `parsing time "today" as "2006-01-02": cannot parse "today" as "2006"`},
				},
			},
			{
				name:   "bad sort_by",
				params: "sort_by=test",
				expectedError: []validationError{
					{Key: "sort_by", Reason: "test is not a valid sort_by. Sort-by must be name or created_at or updated_at"},
				},
			},
		}

		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		for _, te := range tt {
			req, err := http.NewRequest("GET", fmt.Sprintf("/device-groups?%s", te.params), nil)
			Expect(err).ToNot(HaveOccurred())
			w := httptest.NewRecorder()

			ValidateGetAllDeviceGroupsFilterParams(next).ServeHTTP(w, req)

			resp := w.Result()
			var jsonBody []validationError
			err = json.NewDecoder(resp.Body).Decode(&jsonBody)
			Expect(err).ToNot(HaveOccurred())
			for _, exErr := range te.expectedError {
				found := false
				for _, jsErr := range jsonBody {
					if jsErr.Key == exErr.Key && jsErr.Reason == exErr.Reason {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), fmt.Sprintf("in %q: was expected to have %v but not found in %v", te.name, exErr, jsonBody))
			}
		}
	})
	Context("get all device-groups with query parameters", func() {
		tt := []struct {
			name          string
			params        string
			expectedError []validationError
		}{
			{
				name:   "invalid query param",
				params: "bla=1",
				expectedError: []validationError{
					{Key: "bla", Reason: fmt.Sprintf("bla is not a valid query param, supported query params: %s", GetQueryParamsArray("device-groups"))},
				},
			},
			{
				name:   "valid query param and invalid query param",
				params: "sort_by=created_at&bla=1",
				expectedError: []validationError{
					{Key: "bla", Reason: fmt.Sprintf("bla is not a valid query param, supported query params: %s", GetQueryParamsArray("device-groups"))},
				},
			},
			{
				name:   "invalid query param and valid query param",
				params: "bla=1&sort_by=created_at",
				expectedError: []validationError{
					{Key: "bla", Reason: fmt.Sprintf("bla is not a valid query param, supported query params: %s", GetQueryParamsArray("device-groups"))},
				},
			},
		}

		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		for _, te := range tt {
			req, err := http.NewRequest("GET", fmt.Sprintf("/device-groups?%s", te.params), nil)
			Expect(err).ToNot(HaveOccurred())
			w := httptest.NewRecorder()

			ValidateQueryParams("device-groups")(next).ServeHTTP(w, req)

			resp := w.Result()
			var jsonBody []validationError
			err = json.NewDecoder(resp.Body).Decode(&jsonBody)
			Expect(err).ToNot(HaveOccurred())
			for _, exErr := range te.expectedError {
				found := false
				for _, jsErr := range jsonBody {
					if jsErr.Key == exErr.Key && jsErr.Reason == exErr.Reason {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), fmt.Sprintf("in %q: was expected to have %v but not found in %v", te.name, exErr, jsonBody))
			}
		}
	})
	Context("get DeviceGroup by id", func() {
		It("should return 200", func() {
			fakeID, _ := faker.RandomInt(1000, 2000, 1)
			fakeIDUint := uint(fakeID[0])
			req, err := http.NewRequest("GET", "/", nil)
			Expect(err).To(BeNil())

			ctx := context.WithValue(req.Context(), deviceGroupKey, &models.DeviceGroup{
				Model: models.Model{
					ID: fakeIDUint,
				},
			})
			req = req.WithContext(ctx)
			ctx = dependencies.ContextWithServices(req.Context(), &dependencies.EdgeAPIServices{})
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()

			handler := http.HandlerFunc(GetDeviceGroupByID)
			handler.ServeHTTP(rr, req)
			// Check the status code is what we expect.
			Expect(rr.Code).To(Equal(http.StatusOK))
		})
	})
	Context("get DeviceGroup by invalid id", func() {
		It("should return 400", func() {
			req, err := http.NewRequest("GET", "/", nil)
			Expect(err).To(BeNil())

			ctx := context.WithValue(req.Context(), deviceGroupKey, "a")
			req = req.WithContext(ctx)
			ctx = dependencies.ContextWithServices(req.Context(), &dependencies.EdgeAPIServices{})
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()

			handler := http.HandlerFunc(GetDeviceGroupByID)
			handler.ServeHTTP(rr, req)
			// Check the status code is what we expect.
			Expect(rr.Code).To(Equal(http.StatusBadRequest))
		})
	})
	Context("get all devices", func() {
		req, err := http.NewRequest("GET", "/", nil)
		Expect(err).To(BeNil())
		When("all is valid", func() {
			It("should return 200", func() {
				ctx := req.Context()
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().GetDeviceGroupsCount(gomock.Any(), gomock.Any()).Return(int64(0), nil)
				mockDeviceGroupsService.EXPECT().GetDeviceGroups(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&[]models.DeviceGroupListDetail{}, nil)

				handler := http.HandlerFunc(GetAllDeviceGroups)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})
	})
	Context("adding devices to DeviceGroup", func() {
		account := common.DefaultAccount
		orgID := common.DefaultOrgID
		deviceGroupName := faker.Name()
		devices := []models.Device{
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
		}
		deviceGroup := models.DeviceGroup{Name: deviceGroupName, Account: account, OrgID: orgID, Type: models.DeviceGroupTypeDefault}
		Context("adding Devices & DeviceGroup to DB", func() {
			for _, device := range devices {
				dbResult := db.DB.Create(&device).Error
				Expect(dbResult).To(BeNil())
			}
			dbResult := db.DB.Create(&deviceGroup).Error
			Expect(dbResult).To(BeNil())
		})

		Context("get DeviceGroup from DB", func() {
			dbResult := db.Org(orgID, "").First(&deviceGroup).Error
			Expect(dbResult).To(BeNil())
			dbResult = db.Org(orgID, "").Find(&devices).Error
			Expect(dbResult).To(BeNil())
		})
		jsonDeviceBytes, err := json.Marshal(models.DeviceGroup{Devices: devices})
		Expect(err).To(BeNil())

		url := fmt.Sprintf("/%d/devices", deviceGroup.ID)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonDeviceBytes))
		Expect(err).To(BeNil())

		When("all is valid", func() {
			It("should add devices to DeviceGroup", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().AddDeviceGroupDevices(orgID, deviceGroup.ID, gomock.Any()).Return(&devices, nil)

				handler := http.HandlerFunc(AddDeviceGroupDevices)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})
	})
	Context("create DeviceGroup", func() {
		When("all is valid", func() {
			deviceGroup := &models.DeviceGroup{
				Name:    deviceGroupName,
				Type:    models.DeviceGroupTypeDefault,
				Account: common.DefaultAccount,
				OrgID:   common.DefaultOrgID,
			}
			jsonDeviceBytes, err := json.Marshal(deviceGroup)
			Expect(err).To(BeNil())

			url := fmt.Sprintf("/%d", deviceGroup.ID)
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonDeviceBytes))
			Expect(err).To(BeNil())
			It("should create DeviceGroup", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().CreateDeviceGroup(deviceGroup).Return(deviceGroup, nil)

				handler := http.HandlerFunc(CreateDeviceGroup)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})
		When("no account", func() {
			deviceGroup := &models.DeviceGroup{
				Name:    faker.Name(),
				Type:    models.DeviceGroupTypeDefault,
				Account: "",
			}
			jsonDeviceBytes, err := json.Marshal(deviceGroup)
			Expect(err).To(BeNil())

			req, err := http.NewRequest(http.MethodPost, "/", bytes.NewBuffer(jsonDeviceBytes))
			Expect(err).To(BeNil())
			It("should return 400", func() {
				config.Get().Auth = true // enable auth to avoid default account
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				handler := http.HandlerFunc(CreateDeviceGroup)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
				config.Get().Auth = false // disable auth
			})
		})
	})
	Context("update DeviceGroup", func() {
		deviceGroupUpdated := &models.DeviceGroup{
			Name:    deviceGroupName,
			Type:    models.DeviceGroupTypeDefault,
			Account: common.DefaultAccount,
			OrgID:   common.DefaultOrgID,
		}
		jsonDeviceBytes, err := json.Marshal(deviceGroupUpdated)
		Expect(err).To(BeNil())

		url := fmt.Sprintf("/%d", deviceGroupUpdated.ID)
		req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonDeviceBytes))
		Expect(err).To(BeNil())

		When("all is valid", func() {
			It("should update DeviceGroup", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, deviceGroupUpdated)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().GetDeviceGroupByID(fmt.Sprintf("%d", deviceGroupUpdated.ID)).Return(deviceGroupUpdated, nil)
				mockDeviceGroupsService.EXPECT().UpdateDeviceGroup(deviceGroupUpdated, common.DefaultOrgID, fmt.Sprintf("%d", deviceGroupUpdated.ID)).Return(nil)

				handler := http.HandlerFunc(UpdateDeviceGroup)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})

		When("UpdateDeviceGroup return error", func() {
			It("should return internal server error when unknown error occur", func() {
				deviceGroupUpdated := &models.DeviceGroup{Name: deviceGroupName, Type: models.DeviceGroupTypeDefault, OrgID: common.DefaultOrgID}
				jsonDeviceBytes, err := json.Marshal(deviceGroupUpdated)
				Expect(err).To(BeNil())

				url := fmt.Sprintf("/%d", deviceGroupUpdated.ID)
				req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonDeviceBytes))
				Expect(err).To(BeNil())

				ctx := setContextDeviceGroup(req.Context(), deviceGroupUpdated)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				expectedError := errors.New("expected unknown error returned by UpdateDeviceGroup")
				mockDeviceGroupsService.EXPECT().UpdateDeviceGroup(deviceGroupUpdated, common.DefaultOrgID, fmt.Sprintf("%d", deviceGroupUpdated.ID)).Return(expectedError)

				handler := http.HandlerFunc(UpdateDeviceGroup)
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusInternalServerError))
				Expect(rr.Body.String()).To(ContainSubstring("failed updating device group"))
			})

			It("should return bad request when group with same name already exists", func() {
				deviceGroupUpdated := &models.DeviceGroup{Name: deviceGroupName, Type: models.DeviceGroupTypeDefault, OrgID: common.DefaultOrgID}
				jsonDeviceBytes, err := json.Marshal(deviceGroupUpdated)
				Expect(err).To(BeNil())

				url := fmt.Sprintf("/%d", deviceGroupUpdated.ID)
				req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonDeviceBytes))
				Expect(err).To(BeNil())

				ctx := setContextDeviceGroup(req.Context(), deviceGroupUpdated)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				mockDeviceGroupsService.EXPECT().UpdateDeviceGroup(deviceGroupUpdated, common.DefaultOrgID, fmt.Sprintf("%d", deviceGroupUpdated.ID)).Return(new(services.DeviceGroupAlreadyExists))

				handler := http.HandlerFunc(UpdateDeviceGroup)
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
				Expect(rr.Body.String()).To(ContainSubstring(services.DeviceGroupAlreadyExistsMsg))
			})
		})
	})
	Context("delete DeviceGroup", func() {
		account := common.DefaultAccount
		orgID := common.DefaultOrgID
		deviceGroupName := faker.Name()
		devices := []models.Device{
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
		}
		deviceGroup := &models.DeviceGroup{
			Name:    deviceGroupName,
			Type:    models.DeviceGroupTypeDefault,
			Account: account,
			OrgID:   orgID,
			Devices: devices,
		}
		Context("saving DeviceGroup", func() {
			dbResult := db.DB.Create(&deviceGroup).Error
			Expect(dbResult).To(BeNil())
		})
		Context("getting DeviceGroup", func() {
			dbResult := db.Org(orgID, "").First(&deviceGroup).Error
			Expect(dbResult).To(BeNil())
		})
		When("all is valid", func() {
			url := fmt.Sprintf("/%d", deviceGroup.ID)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			Expect(err).To(BeNil())

			It("should return status code 200", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupByID(fmt.Sprintf("%d", deviceGroup.ID)).Return(nil)

				handler := http.HandlerFunc(DeleteDeviceGroupByID)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})
		When("no device group in context", func() {
			url := fmt.Sprintf("/%d", deviceGroup.ID)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			Expect(err).To(BeNil())

			It("should return status code 400", func() {
				ctx := req.Context()
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				handler := http.HandlerFunc(DeleteDeviceGroupByID)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
			})
		})
		When("no account", func() {
			fakeID, _ := faker.RandomInt(1000, 2000, 1)
			fakeIDUint := uint(fakeID[0])
			url := fmt.Sprintf("/%d", fakeIDUint)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			Expect(err).To(BeNil())

			It("should return status code 400", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &models.DeviceGroup{
					Model: models.Model{
						ID: fakeIDUint,
					},
					Account: "",
					OrgID:   orgID,
				})
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupByID(fmt.Sprint(fakeIDUint)).Return(new(services.AccountNotSet))

				handler := http.HandlerFunc(DeleteDeviceGroupByID)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
			})
		})
		When("no orgID", func() {
			fakeID, _ := faker.RandomInt(1000, 2000, 1)
			fakeIDUint := uint(fakeID[0])
			url := fmt.Sprintf("/%d", fakeIDUint)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			Expect(err).To(BeNil())

			It("should return status code 400", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &models.DeviceGroup{
					Model: models.Model{
						ID: fakeIDUint,
					},
					Account: account,
					OrgID:   "",
				})
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupByID(fmt.Sprint(fakeIDUint)).Return(new(services.OrgIDNotSet))

				handler := http.HandlerFunc(DeleteDeviceGroupByID)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
			})
		})
		When("no such ID", func() {
			fakeID, _ := faker.RandomInt(1000, 2000, 1)
			fakeIDUint := uint(fakeID[0])
			url := fmt.Sprintf("/%d", fakeIDUint)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			Expect(err).To(BeNil())

			It("should return status code 404", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &models.DeviceGroup{
					Model: models.Model{
						ID: fakeIDUint,
					},
				})
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupByID(fmt.Sprint(fakeIDUint)).Return(new(services.DeviceGroupNotFound))

				handler := http.HandlerFunc(DeleteDeviceGroupByID)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusNotFound))
			})
		})
		When("something bad happened", func() {
			fakeID, _ := faker.RandomInt(1000, 2000, 1)
			fakeIDUint := uint(fakeID[0])
			url := fmt.Sprintf("/%d", fakeIDUint)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			Expect(err).To(BeNil())

			It("should return status code 500", func() {
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &models.DeviceGroup{
					Model: models.Model{
						ID: fakeIDUint,
					},
				})
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupByID(fmt.Sprint(fakeIDUint)).Return(apiErrors.NewInternalServerError())

				handler := http.HandlerFunc(DeleteDeviceGroupByID)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusInternalServerError))
			})
		})
	})
	Context("delete device from DeviceGroup", func() {
		account := common.DefaultAccount
		orgID := common.DefaultOrgID
		deviceGroupName := faker.Name()
		devices := []models.Device{
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
			},
		}
		deviceGroup := models.DeviceGroup{
			Name:    deviceGroupName,
			Account: account,
			OrgID:   orgID,
			Type:    models.DeviceGroupTypeDefault,
			Devices: devices,
		}

		It("should create device group with devices", func() {
			res := db.DB.Create(&deviceGroup)
			Expect(res.Error).To(BeNil())
			Expect(deviceGroup.ID).NotTo(Equal(0))
		})
		It("load device-group with devices", func() {
			res := db.DB.Preload("Devices").First(&deviceGroup, deviceGroup.ID)
			Expect(res.Error).To(BeNil())
			for _, device := range deviceGroup.Devices {
				// ensure all devices are defined
				Expect(device.ID).NotTo(Equal(0))
			}
		})
		When("device-group and devices are defined", func() {
			It("should delete the first device", func() {
				devicesToRemove := deviceGroup.Devices[:1]
				url := fmt.Sprintf("/%d/devices/%d", deviceGroup.ID, devicesToRemove[0].ID)
				req, err := http.NewRequest(http.MethodDelete, url, nil)
				Expect(err).To(BeNil())

				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &deviceGroup)
				ctx = setContextDeviceGroupDevice(ctx, &devicesToRemove[0])
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupDevices(orgID, deviceGroup.ID, devicesToRemove).Return(&devicesToRemove, nil)
				handler := http.HandlerFunc(DeleteDeviceGroupOneDevice)
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusOK))
			})

			It("should delete the second and third devices", func() {

				devicesToRemove := deviceGroup.Devices[1:]
				devicesToRemoveJSON, err := json.Marshal(models.DeviceGroup{Devices: devicesToRemove})
				Expect(err).To(BeNil())

				url := fmt.Sprintf("/%d/devices", deviceGroup.ID)
				req, err := http.NewRequest(http.MethodDelete, url, bytes.NewBuffer(devicesToRemoveJSON))
				Expect(err).To(BeNil())

				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupDevices(orgID, deviceGroup.ID, gomock.Any()).Return(&devicesToRemove, nil)
				handler := http.HandlerFunc(DeleteDeviceGroupManyDevices)
				handler.ServeHTTP(rr, req)

				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})
		When("sending invalid request body", func() {
			It("should return status code 400", func() {
				url := fmt.Sprintf("/%d/devices", deviceGroup.ID)
				req, err := http.NewRequest(http.MethodDelete, url, bytes.NewBuffer([]byte("{}")))
				Expect(err).To(BeNil())

				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				var devicesToRemove []models.Device
				mockDeviceGroupsService.EXPECT().DeleteDeviceGroupDevices(orgID, deviceGroup.ID, devicesToRemove).Return(nil, new(services.DeviceGroupDevicesNotSupplied))
				handler := http.HandlerFunc(DeleteDeviceGroupManyDevices)
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
			})
		})
	})
	Context("Updating devices from DeviceGroup", func() {
		account := "0000000"
		orgID := "0000000"
		deviceGroupName := faker.Name()
		imageSet := &models.ImageSet{
			Name:    "test",
			Version: 1,
			OrgID:   orgID,
		}
		result := db.DB.Create(imageSet)
		imageV1 := &models.Image{
			Commit: &models.Commit{
				OSTreeCommit: faker.UUIDHyphenated(),
				OrgID:        orgID,
			},
			Status:     models.ImageStatusSuccess,
			ImageSetID: &imageSet.ID,
			Version:    1,
			OrgID:      orgID,
		}
		result = db.DB.Create(imageV1.Commit)
		Expect(result.Error).ToNot(HaveOccurred())
		result = db.DB.Create(imageV1)
		Expect(result.Error).ToNot(HaveOccurred())

		devices := []models.Device{
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
				ImageID: imageV1.ID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
				ImageID: imageV1.ID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
				ImageID: imageV1.ID,
			},
		}

		deviceGroup := models.DeviceGroup{
			Name:    deviceGroupName,
			Account: account,
			OrgID:   orgID,
			Type:    models.DeviceGroupTypeDefault,
			Devices: devices,
		}

		commit := models.Commit{
			Arch:  "x86_64",
			OrgID: orgID,
		}

		When("all is valid with same image Set ID", func() {
			It("should update Devices from Group", func() {
				res := db.DB.Omit("Devices.*").Create(&deviceGroup)
				Expect(res.Error).To(BeNil())
				Expect(deviceGroup.ID).NotTo(Equal(0))
				db.DB.Create(&commit)
				updTransactions := []models.UpdateTransaction{
					{
						Commit:   &commit,
						CommitID: commit.ID,
						Account:  account,
						OrgID:    orgID,
						Devices:  devices,
					},
				}
				url := fmt.Sprintf("/%d/updateDevices", deviceGroup.ID)

				req, err := http.NewRequest(http.MethodPost, url, nil)
				Expect(err).To(BeNil())
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				Expect(err).To(BeNil())
				orgID, err := common.GetOrgID(req)
				Expect(err).To(BeNil())

				var setOfDeviceUUIDS []string
				for _, device := range devices {
					setOfDeviceUUIDS = append(setOfDeviceUUIDS, device.UUID)
				}

				var devicesUpdate models.DevicesUpdate
				devicesUpdate.DevicesUUID = setOfDeviceUUIDS

				var commitID uint
				// setup mock for update
				mockDeviceService.EXPECT().GetLatestCommitFromDevices(orgID, setOfDeviceUUIDS).
					Return(commitID, nil)
				mockCommitService.EXPECT().GetCommitByID(commitID, orgID).
					Return(&commit, nil)
				mockUpdateService.EXPECT().BuildUpdateTransactions(ctx, &devicesUpdate, orgID, &commit).
					Return(&updTransactions, nil)
				for _, trans := range updTransactions {
					mockUpdateService.EXPECT().CreateUpdateAsync(trans.ID)
				}
				handler := http.HandlerFunc(UpdateAllDevicesFromGroup)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))
			})
		})
	})
	Context("Updating devices from DeviceGroup", func() {
		account := "0000000"
		orgID := "0000000"
		deviceGroupName := faker.Name()
		imageSet := &models.ImageSet{
			Name:    "test",
			Version: 1,
			OrgID:   orgID,
		}

		result := db.DB.Create(imageSet)
		imageSet2 := &models.ImageSet{
			Name:    "test2",
			Version: 1,
			OrgID:   orgID,
		}
		result = db.DB.Create(imageSet2)
		Expect(result.Error).ToNot(HaveOccurred())
		imageV1 := &models.Image{
			Commit: &models.Commit{
				OSTreeCommit: faker.UUIDHyphenated(),
				OrgID:        orgID,
			},
			Status:     models.ImageStatusSuccess,
			ImageSetID: &imageSet.ID,
			Version:    1,
			OrgID:      orgID,
		}
		imageV2 := &models.Image{
			Commit: &models.Commit{
				OSTreeCommit: faker.UUIDHyphenated(),
				OrgID:        orgID,
			},
			Status:     models.ImageStatusSuccess,
			ImageSetID: &imageSet2.ID,
			Version:    1,
			OrgID:      orgID,
		}
		result = db.DB.Create(imageV2)
		Expect(result.Error).ToNot(HaveOccurred())

		result = db.DB.Create(imageV1.Commit)
		Expect(result.Error).ToNot(HaveOccurred())
		result = db.DB.Create(imageV1)
		Expect(result.Error).ToNot(HaveOccurred())

		devices := []models.Device{
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
				ImageID: imageV1.ID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
				ImageID: imageV1.ID,
			},
			{
				Name:    faker.Name(),
				UUID:    faker.UUIDHyphenated(),
				Account: account,
				OrgID:   orgID,
				ImageID: imageV2.ID,
			},
		}

		deviceGroup := models.DeviceGroup{
			Name:    deviceGroupName,
			Account: account,
			OrgID:   orgID,
			Type:    models.DeviceGroupTypeDefault,
			Devices: devices,
		}

		Expect(result.Error).ToNot(HaveOccurred())
		commit := models.Commit{
			Arch: "x86_64",
		}

		When("with different imageID", func() {
			It("should not update Devices from Group with different image set ID", func() {
				res := db.DB.Create(&deviceGroup)
				Expect(res.Error).To(BeNil())
				Expect(deviceGroup.ID).NotTo(Equal(0))
				db.DB.Create(&commit)

				url := fmt.Sprintf("/%d/updateDevices", deviceGroup.ID)

				req, err := http.NewRequest(http.MethodPost, url, nil)
				Expect(err).To(BeNil())
				ctx := req.Context()
				ctx = setContextDeviceGroup(ctx, &deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				handler := http.HandlerFunc(UpdateAllDevicesFromGroup)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusBadRequest))
			})
		})
	})

	Context("EnforceEdgeGroups", func() {
		var conf *config.EdgeConfig
		var OrgID string
		// var initialConfigEnforceEdgeGroupsOrgs []string
		var initialAuth bool
		var router *chi.Mux

		BeforeEach(func() {
			conf = config.Get()
			OrgID = faker.UUIDHyphenated()
			// save initial conf values
			initialAuth = conf.Auth

			// initialConfigEnforceEdgeGroupsOrgs = conf.EnforceEdgeGroupsOrgs

			// set config auth to true to force use identity org
			conf.Auth = true
			router = chi.NewRouter()
			router.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx := dependencies.ContextWithServices(r.Context(), edgeAPIServices)
					// set identity orgID
					ctx = testing.WithCustomIdentity(ctx, OrgID)
					next.ServeHTTP(w, r.WithContext(ctx))
				})
			})
			router.Route("/device-groups", MakeDeviceGroupsRouter)

		})

		AfterEach(func() {
			// restore initial conf values
			conf.Auth = initialAuth
			err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return enforce edge groups value true", func() {
			err := os.Setenv(feature.EnforceEdgeGroups.EnvVar, "true")
			Expect(err).ToNot(HaveOccurred())

			req, err := http.NewRequest("GET", "/device-groups/enforce-edge-groups", nil)
			Expect(err).ToNot(HaveOccurred())

			responseRecorder := httptest.NewRecorder()
			router.ServeHTTP(responseRecorder, req)

			Expect(responseRecorder.Code).To(Equal(http.StatusOK))
			respBody, err := io.ReadAll(responseRecorder.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(respBody)).ToNot(BeEmpty())

			var responseDevicesView models.EnforceEdgeGroupsAPI
			err = json.Unmarshal(respBody, &responseDevicesView)
			Expect(err).ToNot(HaveOccurred())
			Expect(responseDevicesView.EnforceEdgeGroups).To(BeTrue())
		})

		It("should return enforce edge groups value false", func() {
			err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
			Expect(err).ToNot(HaveOccurred())

			req, err := http.NewRequest("GET", "/device-groups/enforce-edge-groups", nil)
			Expect(err).ToNot(HaveOccurred())

			responseRecorder := httptest.NewRecorder()
			router.ServeHTTP(responseRecorder, req)

			Expect(responseRecorder.Code).To(Equal(http.StatusOK))
			respBody, err := io.ReadAll(responseRecorder.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(respBody)).ToNot(BeEmpty())

			var responseDevicesView models.EnforceEdgeGroupsAPI
			err = json.Unmarshal(respBody, &responseDevicesView)
			Expect(err).ToNot(HaveOccurred())
			Expect(responseDevicesView.EnforceEdgeGroups).To(BeFalse())
		})

		Context("GetDeviceGroupDetailsByIDView", func() {

			It("should return EnforceEdgeGroups value: true", func() {
				err := os.Setenv(feature.EnforceEdgeGroups.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())
				deviceGroup := models.DeviceGroup{
					OrgID: OrgID, Name: faker.Name(),
					Devices: []models.Device{
						{UUID: faker.UUIDHyphenated(), OrgID: OrgID},
						{UUID: faker.UUIDHyphenated(), OrgID: OrgID},
					},
				}

				mockDeviceGroupsService.EXPECT().GetDeviceGroupByID(fmt.Sprintf("%d", deviceGroup.ID)).Return(&deviceGroup, nil)
				mockDeviceService.EXPECT().GetDevicesCount(gomock.Any()).Return(int64(len(deviceGroup.Devices)), nil)
				mockDeviceService.EXPECT().GetDevicesView(gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.DeviceViewList{}, nil)

				req, err := http.NewRequest("GET", fmt.Sprintf("/device-groups/%d/view", deviceGroup.ID), nil)
				Expect(err).ToNot(HaveOccurred())

				responseRecorder := httptest.NewRecorder()
				router.ServeHTTP(responseRecorder, req)

				Expect(responseRecorder.Code).To(Equal(http.StatusOK))
				respBody, err := io.ReadAll(responseRecorder.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(respBody)).ToNot(BeEmpty())

				var deviceGroupDetails models.DeviceGroupDetailsView
				err = json.Unmarshal(respBody, &deviceGroupDetails)
				Expect(err).ToNot(HaveOccurred())
				Expect(deviceGroupDetails.DeviceDetails.EnforceEdgeGroups).To(BeTrue())
			})

			It("should return EnforceEdgeGroups value: false", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				deviceGroup := models.DeviceGroup{Model: models.Model{ID: 10}, OrgID: OrgID, Name: faker.Name()}

				mockDeviceGroupsService.EXPECT().GetDeviceGroupByID(fmt.Sprintf("%d", deviceGroup.ID)).Return(&deviceGroup, nil)

				req, err := http.NewRequest("GET", fmt.Sprintf("/device-groups/%d/view", deviceGroup.ID), nil)
				Expect(err).ToNot(HaveOccurred())

				responseRecorder := httptest.NewRecorder()
				router.ServeHTTP(responseRecorder, req)

				Expect(responseRecorder.Code).To(Equal(http.StatusOK))
				respBody, err := io.ReadAll(responseRecorder.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(respBody)).ToNot(BeEmpty())

				var deviceGroupDetails models.DeviceGroupDetailsView
				err = json.Unmarshal(respBody, &deviceGroupDetails)
				Expect(err).ToNot(HaveOccurred())
				Expect(deviceGroupDetails.DeviceDetails.EnforceEdgeGroups).To(BeFalse())
			})
		})

		Context("NewFeatureNotAvailable", func() {

			var mockDeviceService *mock_services.MockDeviceGroupsServiceInterface
			var router chi.Router
			var ctrl *gomock.Controller
			var OrgID string
			BeforeEach(func() {
				OrgID = "00000"
				ctrl = gomock.NewController(GinkgoT())

				mockDeviceService = mock_services.NewMockDeviceGroupsServiceInterface(ctrl)
				mockServices := &dependencies.EdgeAPIServices{
					DeviceGroupsService: mockDeviceService,
					Log:                 log.NewEntry(log.StandardLogger()),
				}
				router = chi.NewRouter()
				router.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						fmt.Println(mockServices)
						ctx := dependencies.ContextWithServices(r.Context(), mockServices)
						next.ServeHTTP(w, r.WithContext(ctx))
					})
				})
				router.Route("/", MakeDeviceGroupsRouter)
			})

			AfterEach(func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Unsetenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Unsetenv(feature.HideCreateGroup.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				ctrl.Finish()
			})

			It("should return NewFeatureNotAvailable on create device Groups", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).To(BeNil())
				req, err := http.NewRequest("POST", "/", nil)
				Expect(err).To(BeNil())

				ctx := req.Context()
				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})

				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)

				Expect(rr.Code).To(Equal(http.StatusNotImplemented))

			})
			It("should return NewFeatureNotAvailable on device-groups view ", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())
				deviceGroup := models.DeviceGroup{
					OrgID: OrgID, Name: faker.Name(),
					Devices: []models.Device{
						{UUID: faker.UUIDHyphenated(), OrgID: OrgID},
						{UUID: faker.UUIDHyphenated(), OrgID: OrgID},
					},
				}

				req, err := http.NewRequest("GET", fmt.Sprintf("/device-groups/%d/view", deviceGroup.ID), nil)
				Expect(err).ToNot(HaveOccurred())

				ctx := req.Context()
				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})
				req = req.WithContext(ctx)

				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)

				Expect(rr.Code).To(Equal(http.StatusNotImplemented))

			})

			It("should return NewFeatureNotAvailable on GetDeviceGroupByID ", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())

				fakeID, _ := faker.RandomInt(1000, 2000, 1)
				fakeIDUint := uint(fakeID[0])
				req, err := http.NewRequest("GET", "/", nil)
				Expect(err).To(BeNil())

				ctx := req.Context()
				ctx = context.WithValue(req.Context(), deviceGroupKey, &models.DeviceGroup{
					Model: models.Model{
						ID: fakeIDUint,
					},
				})
				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})
				req = req.WithContext(ctx)

				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusNotImplemented))
			})

			It("should return NewFeatureNotAvailable on UpdateDeviceGroup ", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())

				deviceGroupUpdated := &models.DeviceGroup{
					Name:    deviceGroupName,
					Type:    models.DeviceGroupTypeDefault,
					Account: common.DefaultAccount,
					OrgID:   common.DefaultOrgID,
				}
				jsonDeviceBytes, err := json.Marshal(deviceGroupUpdated)
				Expect(err).To(BeNil())

				url := fmt.Sprintf("/%d", deviceGroupUpdated.ID)
				req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonDeviceBytes))
				Expect(err).To(BeNil())
				ctx := context.WithValue(req.Context(), deviceGroupKey, &models.DeviceGroup{
					Model: models.Model{
						ID: uint(1),
					},
				})
				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})
				req = req.WithContext(ctx)
				ctx = dependencies.ContextWithServices(req.Context(), &dependencies.EdgeAPIServices{})
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusNotImplemented))
			})

			It("should return NewFeatureNotAvailable on DeleteDeviceGroupByID ", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())

				fakeID, _ := faker.RandomInt(1000, 2000, 1)
				fakeIDUint := uint(fakeID[0])
				url := fmt.Sprintf("/%d", fakeIDUint)
				req, err := http.NewRequest(http.MethodDelete, url, nil)
				Expect(err).To(BeNil())
				ctx := req.Context()
				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})
				ctx = setContextDeviceGroup(ctx, &models.DeviceGroup{
					Model: models.Model{
						ID: fakeIDUint,
					},
				})
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)

				Expect(rr.Code).To(Equal(http.StatusNotImplemented))
			})

			It("should return NewFeatureNotAvailable on AddDeviceGroupDevices ", func() {
				err := os.Unsetenv(feature.EnforceEdgeGroups.EnvVar)
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())

				devices := []models.Device{
					{
						Name:    faker.Name(),
						UUID:    faker.UUIDHyphenated(),
						Account: faker.UUIDHyphenated(),
						OrgID:   faker.UUIDHyphenated(),
					},
				}

				jsonDeviceBytes, err := json.Marshal(models.DeviceGroup{Devices: devices})
				Expect(err).To(BeNil())
				req, err := http.NewRequest(http.MethodPost, "/1/devices", bytes.NewBuffer(jsonDeviceBytes))
				Expect(err).To(BeNil())

				ctx := req.Context()
				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)

				Expect(rr.Code).To(Equal(http.StatusNotImplemented))
			})

			It("should return NotAuthorized HideCreategroup:true ", func() {
				err := os.Setenv(feature.EnforceEdgeGroups.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.HideCreateGroup.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())

				req, err := http.NewRequest("POST", "/", nil)
				Expect(err).To(BeNil())

				ctx := req.Context()
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})

				// set identity orgID
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = dependencies.ContextWithServices(ctx, &dependencies.EdgeAPIServices{
					Log: log.NewEntry(log.StandardLogger()),
				})
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusUnauthorized))

			})

			It("should return Success on create DeviceGroup when EdgeEnforce:True", func() {
				err := os.Setenv(feature.EnforceEdgeGroups.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())
				err = os.Setenv(feature.EdgeParityInventoryGroupsEnabled.EnvVar, "true")
				Expect(err).ToNot(HaveOccurred())
				err = os.Unsetenv(feature.HideCreateGroup.EnvVar)
				Expect(err).ToNot(HaveOccurred())

				deviceGroup := &models.DeviceGroup{
					Name:    "test",
					Type:    models.DeviceGroupTypeDefault,
					Account: common.DefaultAccount,
					OrgID:   OrgID,
				}
				jsonDeviceBytes, err := json.Marshal(deviceGroup)
				Expect(err).To(BeNil())

				req, err := http.NewRequest("POST", "/", bytes.NewBuffer(jsonDeviceBytes))
				Expect(err).To(BeNil())

				ctx := req.Context()
				ctx = testing.WithCustomIdentity(ctx, OrgID)
				ctx = setContextDeviceGroup(ctx, deviceGroup)
				ctx = dependencies.ContextWithServices(ctx, edgeAPIServices)
				req = req.WithContext(ctx)
				rr := httptest.NewRecorder()

				// setup mock for DeviceGroupsService
				mockDeviceGroupsService.EXPECT().CreateDeviceGroup(deviceGroup).Return(deviceGroup, nil)

				handler := http.HandlerFunc(CreateDeviceGroup)
				handler.ServeHTTP(rr, req)
				// Check the status code is what we expect.
				Expect(rr.Code).To(Equal(http.StatusOK))

			})

		})
	})
})
