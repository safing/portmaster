import { ChangeDetectionStrategy, Component, Inject, OnInit } from "@angular/core";
import { SfngDialogRef, SFNG_DIALOG_REF } from "../dialog";
import { SfngTipUpService } from "./tipup";
import { ActionRunner, Button, SFNG_TIP_UP_ACTION_RUNNER, TipUp } from './translations';
import { TIPUP_TOKEN } from "./utils";

@Component({
  selector: 'sfng-tipup-container',
  templateUrl: './tipup.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngTipUpComponent implements OnInit, TipUp<any> {
  title: string = 'N/A';
  content: string = 'N/A';
  nextKey?: string;
  buttons?: Button<any>[];
  url?: string;
  urlText: string = 'Read More';

  constructor(
    @Inject(TIPUP_TOKEN) public readonly token: string,
    @Inject(SFNG_DIALOG_REF) private readonly dialogRef: SfngDialogRef<SfngTipUpComponent>,
    @Inject(SFNG_TIP_UP_ACTION_RUNNER) private runner: ActionRunner<any>,
    private tipupService: SfngTipUpService,
  ) { }

  ngOnInit() {
    const doc = this.tipupService.getTipUp(this.token);
    if (!!doc) {
      Object.assign(this, doc);
      this.urlText = doc.urlText || 'Read More';
    }
  }

  async next() {
    if (!this.nextKey) {
      return;
    }

    this.tipupService.open(this.nextKey);
    this.dialogRef.close();
  }

  async runAction(btn: Button<any>) {
    await this.runner.performAction(btn.action);

    // if we have a nextKey for the button but do not do in-app
    // routing we should be able to open the next tipup as soon
    // as the action finished
    if (!!btn.nextKey) {
      this.tipupService.waitFor(btn.nextKey!)
        .subscribe({
          next: () => {
            this.dialogRef.close();
            this.tipupService.open(btn.nextKey!);
          },
          error: console.error
        })
    } else {
      this.close();
    }
  }

  close() {
    this.dialogRef.close();
  }
}
