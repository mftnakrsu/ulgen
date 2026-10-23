import { Composition, Folder } from "remotion";
import { ClaudeScene } from "./scenes/ClaudeScene";
import { HealthScene } from "./scenes/HealthScene";
import { KeyScene } from "./scenes/KeyScene";
import { LoginScene } from "./scenes/LoginScene";
import { OpeningScene } from "./scenes/OpeningScene";
import { OutroScene } from "./scenes/OutroScene";
import { SetupScene } from "./scenes/SetupScene";
import { GalleryStill } from "./stills/GalleryStill";
import { UlgenDemo } from "./UlgenDemo";

export const RemotionRoot: React.FC = () => {
  return (
    <>
      <Folder name="Demo">
        <Composition id="UlgenDemo-en" component={UlgenDemo} durationInFrames={1215} fps={30} width={1920} height={1080} defaultProps={{ lang: "en", captions: false }} />
        <Composition id="UlgenDemo-tr" component={UlgenDemo} durationInFrames={1215} fps={30} width={1920} height={1080} defaultProps={{ lang: "tr", captions: false }} />
      </Folder>
      <Folder name="Scenes">
        <Composition id="Opening" component={OpeningScene} durationInFrames={105} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
        <Composition id="Login" component={LoginScene} durationInFrames={195} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
        <Composition id="Setup" component={SetupScene} durationInFrames={210} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
        <Composition id="Health" component={HealthScene} durationInFrames={225} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
        <Composition id="Claude" component={ClaudeScene} durationInFrames={300} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
        <Composition id="Key" component={KeyScene} durationInFrames={135} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
        <Composition id="Outro" component={OutroScene} durationInFrames={135} fps={30} width={1920} height={1080} defaultProps={{ lang: "en" }} />
      </Folder>
      <Folder name="Stills">
        <Composition id="still-doctor" component={GalleryStill} durationInFrames={1} fps={30} width={2160} height={1350} defaultProps={{ which: "doctor", lang: "en" }} />
        <Composition id="still-status" component={GalleryStill} durationInFrames={1} fps={30} width={2160} height={1350} defaultProps={{ which: "status", lang: "en" }} />
        <Composition id="still-setup" component={GalleryStill} durationInFrames={1} fps={30} width={2160} height={1350} defaultProps={{ which: "setup", lang: "en" }} />
        <Composition id="still-claude" component={GalleryStill} durationInFrames={1} fps={30} width={2160} height={1350} defaultProps={{ which: "claude", lang: "en" }} />
      </Folder>
    </>
  );
};
