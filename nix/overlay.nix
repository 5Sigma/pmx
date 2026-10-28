final: prev:
let
  go = final.go_1_26 or final.go;
  drv = final.callPackage ./package.nix {
    buildGoModule = final.buildGoModule.override { inherit go; };
  };
in {
  pmx  = drv;
  pmxd = drv;
}
