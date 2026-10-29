{
  description = "pmx — a keyboard-driven terminal email client";

  inputs = {
    nixpkgs.url    = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    let
      overlay = import ./nix/overlay.nix self;
    in
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ overlay ];
        };
      in {
        packages = {
          inherit (pkgs) pmx;
          default = pkgs.pmx;
        };

        apps = {
          pmx     = { type = "app"; program = "${pkgs.pmx}/bin/pmx"; };
          default = { type = "app"; program = "${pkgs.pmx}/bin/pmx"; };
        };

        devShells.default = pkgs.mkShell {
          packages = [
            (pkgs.go_1_26 or pkgs.go)
            pkgs.gopls
            pkgs.golangci-lint
            pkgs.sqlite
          ];
        };

        formatter = pkgs.nixfmt-rfc-style;

        checks = {
          inherit (pkgs) pmx;
        };
      }
    ) // {
      overlays.default = overlay;

      homeManagerModules.pmxd    = import ./nix/hm-module.nix self;
      homeManagerModules.default = self.homeManagerModules.pmxd;
    };
}
