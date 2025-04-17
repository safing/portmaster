import { NgModule } from "@angular/core";
import { BytesPipe } from "./bytes.pipe";
import { TimeAgoPipe } from "./time-ago.pipe";
import { ToAppProfilePipe } from "./to-profile.pipe";
import { DurationPipe } from "./duration.pipe";
import { RoundPipe } from "./round.pipe";
import { ToSecondsPipe } from "./to-seconds.pipe";
import { HttpImgSrcPipe } from "./http-img-src.pipe";

@NgModule({
  declarations: [
    TimeAgoPipe,
    BytesPipe,
    ToAppProfilePipe,
    DurationPipe,
    RoundPipe,
    ToSecondsPipe,
    HttpImgSrcPipe
  ],
  exports: [
    TimeAgoPipe,
    BytesPipe,
    ToAppProfilePipe,
    DurationPipe,
    RoundPipe,
    ToSecondsPipe,
    HttpImgSrcPipe
  ]
})
export class CommonPipesModule { }
