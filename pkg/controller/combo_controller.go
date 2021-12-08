package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/operator-framework/combo/api/v1alpha1"
	combinationPkg "github.com/operator-framework/combo/pkg/combination"
	comboConditions "github.com/operator-framework/combo/pkg/conditions"
	templatePkg "github.com/operator-framework/combo/pkg/template"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	RequeueDefaultTime = time.Second * 2
	SuccessCondition   = comboConditions.ProccessedCombinationsCondition
)

type combinationController struct {
	client.Client
	log logr.Logger
}

// manageWith creates a new instance of this controller
func (c *combinationController) manageWith(mgr ctrl.Manager, version int) error {
	c.log = c.log.V(version)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Combination{}).
		Complete(c)
}

// Reconcile manages incoming combination CR's and processes them accordingly
func (c *combinationController) Reconcile(ctx context.Context, req ctrl.Request) (result reconcile.Result, deferredErr error) {
	// Set up a convenient log object so we don’t have to type request over and over again
	log := c.log.WithValues("request", req)

	log.Info("new combination event inbound")

	// Attempt to retrieve the requested combination CR
	combination := &v1alpha1.Combination{}
	err := c.Get(ctx, req.NamespacedName, combination)
	if err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// If combination is being deleted, remove from queue
	if !combination.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("combination is being deleted, ignoring event")
		return reconcile.Result{}, nil
	}

	log.Info(fmt.Sprintf("combination %s successfully found in reconciler", combination.Name))

	// Attempt to retrieve the template referenced in the combination CR
	template := &v1alpha1.Template{}
	templateQuery := types.NamespacedName{Name: combination.Spec.Template}
	if err := c.Get(ctx, templateQuery, template); err != nil {
		err = fmt.Errorf("failed to retrieve %v template: %w", combination.Spec.Template, err)
		return c.manageFailure(ctx, combination, comboConditions.TemplateNotFoundCondition, err)
	}

	// Build combination stream to be utilized in template builder
	comboStream := combinationPkg.NewStream(
		combinationPkg.WithArgs(formatArguments(combination.Spec.Arguments)),
		combinationPkg.WithSolveAhead(),
	)

	// Create a new template builder
	builder, err := templatePkg.NewBuilder(strings.NewReader(template.Spec.Body), comboStream)
	if err != nil {
		err = fmt.Errorf("failed to construct a builder out of %s template body: %w", template.Name, err)
		return c.manageFailure(ctx, combination, comboConditions.TemplateBodyInvalid, err)
	}

	// Build the manifest combinations
	generatedManifests, err := builder.Build(ctx)
	if err != nil {
		err = fmt.Errorf("failed to generate manifest %s combinations: %w", combination.Name, err)
		return c.manageFailure(ctx, combination, comboConditions.ManifestGenerationFailed, err)
	}

	log.Info(fmt.Sprintf("reconciliation of %s combination complete!", combination.Name))

	// Return and update the combination's status
	return c.manageSuccess(ctx, combination, generatedManifests)
}

// manageSuccess takes a combination CR and its evaluation to process a successful status update for it
func (c *combinationController) manageSuccess(ctx context.Context, combination *v1alpha1.Combination, evaluation []string) (reconcile.Result, error) {
	// Create the new status condition to transition to
	combination.Status.Conditions = comboConditions.NewConditions(time.Now(), nil, SuccessCondition)

	combination.Status.Evaluation = evaluation

	// Update the status of the combination, requeue if this update fails
	updateErr := c.Status().Update(ctx, combination)
	if updateErr != nil {
		c.log.Info("Error when updating success status, requeuing: " + updateErr.Error())
		return reconcile.Result{RequeueAfter: RequeueDefaultTime}, updateErr
	}

	return reconcile.Result{}, nil
}

// manageFailure takes a combination CR, the new condition and whatever error occurred to process a failure status update for it
func (c *combinationController) manageFailure(ctx context.Context, combination *v1alpha1.Combination, condition metav1.Condition, err error) (reconcile.Result, error) {
	// Create the new status condition to transition to
	combination.Status.Conditions = comboConditions.NewConditions(time.Now(), err, condition)

	// Update the status of the combination, requeue if this update fails
	updateErr := c.Status().Update(ctx, combination)
	if updateErr != nil {
		c.log.Info("Error when updating failure status, requeuing: " + updateErr.Error())
		return reconcile.Result{RequeueAfter: RequeueDefaultTime}, updateErr
	}

	return reconcile.Result{}, err
}

// formatArguments takes the arguments for the combination and formats them ito what the combination package
// is expecting
func formatArguments(arguments []v1alpha1.Argument) map[string][]string {
	formattedArguments := map[string][]string{}
	for _, argument := range arguments {
		formattedArguments[argument.Key] = argument.Values
	}
	return formattedArguments
}
