package user

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/jenkinsci/kubernetes-operator/pkg/apis/jenkinsio/v1alpha1"
	"github.com/jenkinsci/kubernetes-operator/pkg/controller/jenkins/configuration/user/seedjobs"
	"github.com/jenkinsci/kubernetes-operator/pkg/log"

	"github.com/go-logr/logr"
	stackerr "github.com/pkg/errors"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// Validate validates Jenkins CR Spec section
func (r *ReconcileUserConfiguration) Validate(jenkins *v1alpha1.Jenkins) (bool, error) {
	valid, err := r.validateSeedJobs(jenkins)
	if !valid || err != nil {
		return valid, err
	}

	return true, nil
}

func (r *ReconcileUserConfiguration) validateSeedJobs(jenkins *v1alpha1.Jenkins) (bool, error) {
	valid := true

	// TODO id must be unique
	if jenkins.Spec.SeedJobs != nil {
		for _, seedJob := range jenkins.Spec.SeedJobs {
			logger := r.logger.WithValues("seedJob", fmt.Sprintf("%+v", seedJob)).V(log.VWarn)

			if len(seedJob.ID) == 0 {
				logger.Info("id can't be empty")
				valid = false
			}

			if len(seedJob.RepositoryBranch) == 0 {
				logger.Info("repository branch can't be empty")
				valid = false
			}

			if len(seedJob.RepositoryURL) == 0 {
				logger.Info("repository URL branch can't be empty")
				valid = false
			}

			if len(seedJob.Targets) == 0 {
				logger.Info("targets can't be empty")
				valid = false
			}

			if _, ok := v1alpha1.AllowedJenkinsCredentialMap[string(seedJob.JenkinsCredentialType)]; !ok {
				logger.Info("unknown credential type")
				return false, nil
			}

			if (seedJob.JenkinsCredentialType == v1alpha1.BasicSSHCredentialType ||
				seedJob.JenkinsCredentialType == v1alpha1.UsernamePasswordCredentialType) && len(seedJob.CredentialID) == 0 {
				logger.Info("credential ID can't be empty")
				valid = false
			}

			// validate repository url match private key
			if strings.Contains(seedJob.RepositoryURL, "git@") && seedJob.JenkinsCredentialType == v1alpha1.NoJenkinsCredentialCredentialType {
				logger.Info("Jenkins credential must be set while using ssh repository url")
				valid = false
			}

			if seedJob.JenkinsCredentialType == v1alpha1.BasicSSHCredentialType || seedJob.JenkinsCredentialType == v1alpha1.UsernamePasswordCredentialType {
				secret := &v1.Secret{}
				namespaceName := types.NamespacedName{Namespace: jenkins.Namespace, Name: seedJob.CredentialID}
				err := r.k8sClient.Get(context.TODO(), namespaceName, secret)
				if err != nil && apierrors.IsNotFound(err) {
					logger.Info(fmt.Sprintf("required secret '%s' with Jenkins credential not found", seedJob.CredentialID))
					return false, nil
				} else if err != nil {
					return false, stackerr.WithStack(err)
				}

				if seedJob.JenkinsCredentialType == v1alpha1.BasicSSHCredentialType {
					if ok := validateBasicSSHSecret(logger, *secret); !ok {
						valid = false
					}
				}
				if seedJob.JenkinsCredentialType == v1alpha1.UsernamePasswordCredentialType {
					if ok := validateUsernamePasswordSecret(logger, *secret); !ok {
						valid = false
					}
				}
			}
		}
	}
	return valid, nil
}

func validateBasicSSHSecret(logger logr.InfoLogger, secret v1.Secret) bool {
	valid := true
	username, exists := secret.Data[seedjobs.UsernameSecretKey]
	if !exists {
		logger.Info(fmt.Sprintf("required data '%s' not found in secret '%s'", seedjobs.UsernameSecretKey, secret.ObjectMeta.Name))
		valid = false
	}
	if len(username) == 0 {
		logger.Info(fmt.Sprintf("required data '%s' is empty in secret '%s'", seedjobs.UsernameSecretKey, secret.ObjectMeta.Name))
		valid = false
	}

	privateKey, exists := secret.Data[seedjobs.PrivateKeySecretKey]
	if !exists {
		logger.Info(fmt.Sprintf("required data '%s' not found in secret '%s'", seedjobs.PrivateKeySecretKey, secret.ObjectMeta.Name))
		valid = false
	}
	if len(string(privateKey)) == 0 {
		logger.Info(fmt.Sprintf("required data '%s' not found in secret '%s'", seedjobs.PrivateKeySecretKey, secret.ObjectMeta.Name))
		return false
	}
	if err := validatePrivateKey(string(privateKey)); err != nil {
		logger.Info(fmt.Sprintf("private key '%s' invalid in secret '%s': %s", seedjobs.PrivateKeySecretKey, secret.ObjectMeta.Name, err))
		valid = false
	}

	return valid
}

func validateUsernamePasswordSecret(logger logr.InfoLogger, secret v1.Secret) bool {
	valid := true
	username, exists := secret.Data[seedjobs.UsernameSecretKey]
	if !exists {
		logger.Info(fmt.Sprintf("required data '%s' not found in secret '%s'", seedjobs.UsernameSecretKey, secret.ObjectMeta.Name))
		valid = false
	}
	if len(username) == 0 {
		logger.Info(fmt.Sprintf("required data '%s' is empty in secret '%s'", seedjobs.UsernameSecretKey, secret.ObjectMeta.Name))
		valid = false
	}
	password, exists := secret.Data[seedjobs.PasswordSecretKey]
	if !exists {
		logger.Info(fmt.Sprintf("required data '%s' not found in secret '%s'", seedjobs.PasswordSecretKey, secret.ObjectMeta.Name))
		valid = false
	}
	if len(password) == 0 {
		logger.Info(fmt.Sprintf("required data '%s' is empty in secret '%s'", seedjobs.PasswordSecretKey, secret.ObjectMeta.Name))
		valid = false
	}

	return valid
}

func validatePrivateKey(privateKey string) error {
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return stackerr.New("failed to decode PEM block")
	}

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return stackerr.WithStack(err)
	}

	err = priv.Validate()
	if err != nil {
		return stackerr.WithStack(err)
	}

	return nil
}
