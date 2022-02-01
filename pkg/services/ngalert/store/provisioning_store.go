package store

import (
	"context"
	"fmt"

	"github.com/grafana/grafana/pkg/services/ngalert/models"
	"github.com/grafana/grafana/pkg/services/sqlstore"
)

type provenanceRecord struct {
	Id         int   `xorm:"pk autoincr 'id'"`
	OrgID      int64 `xorm:"'org_id'"`
	RecordKey  string
	RecordType string
	Provenance models.Provenance
}

func (pr provenanceRecord) TableName() string {
	return "provenance_type"
}

// ProvisioningStore is a store of provisioning data for arbitrary objects.
type ProvisioningStore interface {
	GetProvenance(ctx context.Context, o models.Provisionable) (models.Provenance, error)
	// TODO: API to query all provenances for a specific type?
	SetProvenance(ctx context.Context, o models.Provisionable, p models.Provenance) error
}

type TransactionalProvisioningStore interface {
	GetProvenance(ctx context.Context, o models.Provisionable) (models.Provenance, error)
	// TODO: API to query all provenances for a specific type?
	SetProvenanceTransactional(o models.Provisionable, p models.Provenance, uow UnitOfWork) UnitOfWork
}

func (st DBstore) GetProvenance(ctx context.Context, o models.Provisionable) (models.Provenance, error) {
	recordType := o.ResourceType()
	recordKey := o.ResourceID()
	orgID := o.ResourceOrgID()

	provenance := models.ProvenanceNone
	err := st.SQLStore.WithDbSession(ctx, func(sess *sqlstore.DBSession) error {
		filter := "record_key = ? AND record_type = ? AND org_id = ?"
		var result models.Provenance
		has, err := sess.Table(provenanceRecord{}).Where(filter, recordKey, recordType, orgID).Desc("id").Cols("provenance").Get(&result)
		if err != nil {
			return fmt.Errorf("failed to query for existing provenance status: %w", err)
		}
		if has {
			provenance = result
		}
		return nil
	})
	if err != nil {
		return models.ProvenanceNone, err
	}
	return provenance, nil
}

func (st DBstore) SetProvenance(ctx context.Context, o models.Provisionable, p models.Provenance) error {
	xact := NewTransaction(st.SQLStore)
	xact = st.SetProvenanceTransactional(o, p, xact)
	return xact.Execute(ctx)
}

func (st DBstore) SetProvenanceTransactional(o models.Provisionable, p models.Provenance, uow UnitOfWork) UnitOfWork {
	recordType := o.ResourceType()
	recordKey := o.ResourceID()
	orgID := o.ResourceOrgID()

	uow = uow.Do(func(sess *sqlstore.DBSession) error {
		// TODO: Need to make sure that writing a record where our concurrency key fails will also fail the whole transaction. That way, this gets rolled back too. can't just check that 0 updates happened inmemory. Check with jp. If not possible, we need our own concurrency key.
		// TODO: Clean up stale provenance records periodically.
		filter := "record_key = ? AND record_type = ? AND org_id = ?"
		_, err := sess.Table(provenanceRecord{}).Where(filter, recordKey, recordType, orgID).Delete(provenanceRecord{})

		if err != nil {
			return fmt.Errorf("failed to delete pre-existing provisioning status: %w", err)
		}

		record := provenanceRecord{
			RecordKey:  recordKey,
			RecordType: recordType,
			Provenance: p,
			OrgID:      orgID,
		}

		if _, err := sess.Insert(record); err != nil {
			return fmt.Errorf("failed to store provisioning status: %w", err)
		}

		return nil
	})
	return uow
}