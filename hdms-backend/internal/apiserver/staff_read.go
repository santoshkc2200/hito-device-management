package apiserver

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

// staffDeviceView is deliberately not the admin DeviceSummary. It names the
// device and when it is expected back, and carries no borrower at all — the
// type system is doing the privacy work here, so a future field on the admin
// shape cannot leak through this endpoint by accident.
func staffDeviceView(d catalogapi.DeviceSummary, openLoan *lendingapi.Loan, categoryName string) gen.StaffDevice {
	devID, _ := uuid.Parse(d.ID)
	view := gen.StaffDevice{
		Id:           devID,
		AssetTag:     d.AssetTag,
		Name:         d.Name,
		Availability: gen.StaffDeviceAvailabilityAvailable,
	}
	if d.Model != "" {
		view.Model = &d.Model
	}
	if categoryName != "" {
		view.CategoryName = &categoryName
	}
	if d.Status != catalogapi.StatusAvailable && d.Status != catalogapi.StatusOnLoan {
		view.Availability = gen.StaffDeviceAvailabilityUnavailable
		return view
	}
	if openLoan != nil {
		view.Availability = gen.StaffDeviceAvailabilityInUse
		view.ExpectedBackAt = openLoan.DueAt
	}
	return view
}

func (s *Server) GetStaffDevices(w http.ResponseWriter, r *http.Request) {
	result, err := s.catalog.ListDevices(r.Context(), catalogapi.ListDevicesParams{
		Limit: 1000,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	categoryNames := make(map[string]string)
	if cats, err := s.catalog.ListCategories(r.Context()); err == nil {
		for _, cat := range cats {
			categoryNames[cat.ID] = cat.Name
		}
	}

	items := make([]gen.StaffDevice, 0, len(result.Items))
	for _, d := range result.Items {
		var openLoan *lendingapi.Loan
		loan, err := s.lending.HolderOf(r.Context(), d.ID)
		if err == nil {
			openLoan = &loan
		} else if !errors.Is(err, lendingapi.ErrLoanNotFound) {
			s.writeServiceError(w, r, err)
			return
		}

		catName := categoryNames[d.CategoryID]
		items = append(items, staffDeviceView(d, openLoan, catName))
	}

	writeJSON(w, http.StatusOK, gen.StaffDeviceList{
		Items:      items,
		NextCursor: strPtr(result.NextCursor),
	})
}

func (s *Server) GetStaffDevice(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	device, err := s.catalog.LookupDevice(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var openLoan *lendingapi.Loan
	loan, err := s.lending.HolderOf(r.Context(), id)
	if err == nil {
		openLoan = &loan
	} else if !errors.Is(err, lendingapi.ErrLoanNotFound) {
		s.writeServiceError(w, r, err)
		return
	}

	var catName string
	if device.CategoryID != "" {
		if cat, err := s.catalog.CategoryOf(r.Context(), device.CategoryID); err == nil {
			catName = cat.Name
		}
	}

	writeJSON(w, http.StatusOK, staffDeviceView(device, openLoan, catName))
}

func (s *Server) GetStaffMeLoans(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}

	res, err := s.lending.ListLoans(r.Context(), lendingapi.ListLoansParams{
		UserID: account.UserID,
		Limit:  100,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.StaffLoan, 0, len(res.Items))
	for _, l := range res.Items {
		var assetTag, devName string
		if dev, err := s.catalog.LookupDevice(r.Context(), l.DeviceID); err == nil {
			assetTag = dev.AssetTag
			devName = dev.Name
		}

		loanID, _ := uuid.Parse(l.ID)
		devID, _ := uuid.Parse(l.DeviceID)
		items = append(items, gen.StaffLoan{
			Id:             loanID,
			DeviceId:       devID,
			DeviceAssetTag: assetTag,
			DeviceName:     devName,
			BorrowedAt:     l.BorrowedAt,
			DueAt:          l.DueAt,
			ReturnedAt:     l.ReturnedAt,
			Status:         gen.LoanStatus(l.Status),
		})
	}

	writeJSON(w, http.StatusOK, gen.StaffLoanList{
		Items:      items,
		NextCursor: strPtr(res.NextCursor),
	})
}

func (s *Server) GetStaffMeReservations(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}

	statusActive := reservationsapi.StatusActive
	res, err := s.reservations.ListUserReservations(r.Context(), account.UserID, &statusActive, nil, nil, 100)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.StaffReservation, 0, len(res.Items))
	for _, rsv := range res.Items {
		resID, _ := uuid.Parse(rsv.ID)
		devID, _ := uuid.Parse(rsv.DeviceID)
		items = append(items, gen.StaffReservation{
			Id:             resID,
			DeviceId:       devID,
			DeviceAssetTag: rsv.DeviceAssetTag,
			DeviceName:     rsv.DeviceName,
			StartAt:        rsv.StartAt,
			EndAt:          rsv.EndAt,
			Status:         gen.ReservationStatus(rsv.Status),
		})
	}

	writeJSON(w, http.StatusOK, gen.StaffReservationList{
		Items: items,
	})
}

func (s *Server) GetStaffMeCredential(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}

	creds, err := s.credentials.ListBySubject(r.Context(), credentialsapi.SubjectUser, account.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var activeQR *credentialsapi.Credential
	for _, c := range creds {
		if c.Status == credentialsapi.StatusActive && c.Kind == credentialsapi.KindQR {
			activeQR = &c
			break
		}
	}
	if activeQR == nil {
		p := httpx.NewProblem("no-credential", "No active credential", http.StatusNotFound)
		p.Detail = "Ask an administrator for a card."
		httpx.WriteProblem(w, r, p)
		return
	}

	issued, err := s.credentials.Reveal(r.Context(), activeQR.ID, "staff:"+account.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, issuedCredentialToGen(issued))
}
