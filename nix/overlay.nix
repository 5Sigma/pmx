self:
final: prev:
let
  go     = final.go_1_26 or final.go;
  gitRev = self.shortRev or "unknown";
  drv    = final.callPackage ./package.nix {
    buildGoModule = final.buildGoModule.override { inherit go; };
    inherit gitRev;
  };
in {
  pmx = drv;
}
