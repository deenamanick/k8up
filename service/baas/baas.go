package baas

import (
	"sync"

	"git.vshn.net/vshn/baas/monitoring"

	"git.vshn.net/vshn/baas/log"

	backupv1alpha1 "git.vshn.net/vshn/baas/apis/backup/v1alpha1"
	baas8scli "git.vshn.net/vshn/baas/client/k8s/clientset/versioned"
	cron "github.com/Infowatch/cron"
	"k8s.io/client-go/kubernetes"
)

// Syncer is the interface that each Baas implementation has to satisfy.
type Syncer interface {
	// EnsurePodTerminator will ensure that the pod terminator is running and working.
	EnsureBackup(pt *backupv1alpha1.Backup) error
	// DeletePodTerminator will stop and delete the pod terminator.
	DeleteBackup(name string) error
}

// Baas will ensure that the backups are running accordingly.
type Baas struct {
	k8sCli  kubernetes.Interface
	baasCLI baas8scli.Interface
	reg     sync.Map
	logger  log.Logger
	cron    *cron.Cron
}

// NewBaas returns a new baas.
func NewBaas(k8sCli kubernetes.Interface, baasCLI baas8scli.Interface, logger log.Logger) *Baas {
	cron := cron.New()
	cron.Start()
	return &Baas{
		k8sCli:  k8sCli,
		baasCLI: baasCLI,
		reg:     sync.Map{},
		logger:  logger,
		cron:    cron,
	}
}

// EnsureBackup satisfies Syncer interface.
func (b *Baas) EnsureBackup(backup *backupv1alpha1.Backup) error {
	var ok bool
	name := backup.Namespace + "/" + backup.Name
	tmpBck, ok := b.reg.Load(name)
	var bck Backupper

	// We are already running.
	if ok {
		bck = tmpBck.(Backupper)
		// If not the same spec means options have changed, so we don't longer need this Backup.
		if !bck.SameSpec(backup) {
			b.logger.Infof("spec of %s changed, recreating baas worker", backup.Name)
			if err := b.DeleteBackup(name); err != nil {
				return err
			}
		} else { // We are ok, nothing changed.
			return nil
		}
	}

	metrics := newOperatorMetrics(monitoring.GetInstance())

	// Create a Backup.
	backupCopy := backup.DeepCopy()
	bck = NewPVCBackupper(backupCopy, b.k8sCli, b.baasCLI, b.logger, b.cron, metrics)
	b.reg.Store(name, bck)
	return bck.Start()
}

// DeleteBackup satisfies Syncer interface.
func (b *Baas) DeleteBackup(name string) error {
	pkt, ok := b.reg.Load(name)
	if !ok {
		return nil
	}

	pk := pkt.(Backupper)
	if err := pk.Stop(); err != nil {
		return err
	}

	b.reg.Delete(name)
	return nil
}