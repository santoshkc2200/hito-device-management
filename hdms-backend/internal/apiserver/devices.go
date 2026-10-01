package apiserver

import (
	"context"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

func (s *Server) ListDevices(w http.ResponseWriter, r *http.Request, params gen.ListDevicesParams) {
	lp, err := listing.Parse(r, listing.DevicesSpec)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	categoryID := fromPtr(params.Category)
	if categoryID == "" {
		categoryID = lp.Filter("categoryId")
		if categoryID == "" {
			categoryID = lp.Filter("category_id")
		}
	}

	statusStr := fromDeviceStatusFilter(params.Status)
	if statusStr == "" {
		statusStr = lp.Filter("status")
	}

	qStr := fromPtr(params.Q)
	if qStr == "" {
		qStr = lp.Filter("q")
	}

	result, err := s.catalog.ListDevices(r.Context(), catalogapi.ListDevicesParams{
		Status:     catalogapi.DeviceStatus(statusStr),
		CategoryID: categoryID,
		Query:      qStr,
		Cursor:     lp.RawCursor,
		Limit:      lp.Limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Device, 0, len(result.Items))
	for _, d := range result.Items {
		items = append(items, deviceToGen(d))
	}
	writeJSON(w, http.StatusOK, gen.DeviceList{Items: items, NextCursor: strPtr(result.NextCursor)})
}

func (s *Server) CreateDevice(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.CreateDeviceRequest](w, r)
	if !ok {
		return
	}

	actor := actorFrom(r)

	// A device is created with its QR label in one transaction, so it can be
	// scanned from the moment it exists and never sits without a credential.
	var device catalogapi.DeviceSummary
	err := db.NewTxManager(s.pool).Do(r.Context(), func(ctx context.Context) error {
		var err error
		device, err = s.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
			AssetTag:     req.AssetTag,
			Name:         req.Name,
			CategoryID:   req.CategoryId,
			Manufacturer: fromPtr(req.Manufacturer),
			Model:        fromPtr(req.Model),
			SerialNo:     fromPtr(req.SerialNo),
			HomeLocation: fromPtr(req.HomeLocation),
			Notes:        fromPtr(req.Notes),
			AcquiredOn:   dateToTimePtr(req.AcquiredOn),
		}, actor)
		if err != nil {
			return err
		}
		_, err = s.credentials.Issue(ctx, credentialsapi.IssueParams{
			SubjectType: credentialsapi.SubjectDevice,
			SubjectID:   device.ID,
			Kind:        credentialsapi.KindQR,
			IssuedBy:    actor,
		})
		return err
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, deviceToGen(device))
}

func (s *Server) GetDevice(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	device, err := s.catalog.LookupDevice(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deviceToGen(device))
}

func (s *Server) UpdateDevice(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.UpdateDeviceRequest](w, r)
	if !ok {
		return
	}

	actor := actorFrom(r)
	device, err := s.catalog.UpdateDevice(r.Context(), id, catalogapi.UpdateDeviceParams{
		Name:         req.Name,
		CategoryID:   req.CategoryId,
		Manufacturer: fromPtr(req.Manufacturer),
		Model:        fromPtr(req.Model),
		SerialNo:     fromPtr(req.SerialNo),
		HomeLocation: fromPtr(req.HomeLocation),
		Notes:        fromPtr(req.Notes),
		AcquiredOn:   dateToTimePtr(req.AcquiredOn),
	}, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	if req.Condition != nil {
		device, err = s.catalog.SetCondition(r.Context(), id, catalogapi.DeviceCondition(*req.Condition), actor)
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, deviceToGen(device))
}

func (s *Server) SetDeviceStatus(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.SetDeviceStatusRequest](w, r)
	if !ok {
		return
	}

	device, err := s.catalog.SetStatus(r.Context(), id, catalogapi.DeviceStatus(req.Status), fromPtr(req.Reason), actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deviceToGen(device))
}

func deviceToGen(d catalogapi.DeviceSummary) gen.Device {
	return gen.Device{
		Id:           d.ID,
		AssetTag:     d.AssetTag,
		Name:         d.Name,
		CategoryId:   d.CategoryID,
		Manufacturer: strPtr(d.Manufacturer),
		Model:        strPtr(d.Model),
		SerialNo:     strPtr(d.SerialNo),
		Status:       gen.DeviceStatus(d.Status),
		Condition:    gen.DeviceCondition(d.Condition),
		HomeLocation: strPtr(d.HomeLocation),
		Notes:        strPtr(d.Notes),
		AcquiredOn:   timePtrToDate(d.AcquiredOn),
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
	}
}

func fromDeviceStatusFilter(f *gen.DeviceStatusFilter) string {
	if f == nil {
		return ""
	}
	return string(*f)
}

func dateToTimePtr(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

func timePtrToDate(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}
