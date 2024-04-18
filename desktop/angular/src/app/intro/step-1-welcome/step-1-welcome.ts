import { ChangeDetectionStrategy, Component, Inject, TemplateRef, ViewChild } from "@angular/core";
import { Step, StepRef, STEP_REF } from "@safing/ui";
import { of } from "rxjs";

@Component({
  templateUrl: './step-1-welcome.html',
  styleUrls: ['../step.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class Step1WelcomeComponent implements Step {
  validChange = of(true)

  readonly nextButtonLabel = 'Quick Setup';

  @ViewChild('buttonTemplate', { static: true })
  buttonTemplate!: TemplateRef<any>;

  constructor(
    @Inject(STEP_REF) public stepRef: StepRef<void>,
  ) { }
}

