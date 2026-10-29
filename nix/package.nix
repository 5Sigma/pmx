# To update vendorHash after go.sum changes:
#   1. Set vendorHash to lib.fakeHash (or "") below.
#   2. Run: nix build .#pmx
#   3. The build fails and prints: "got: sha256-XXXX..."
#   4. Paste that sha256 value here and rebuild.
{ lib
, buildGoModule
, makeWrapper
, xdg-utils
, gitRev ? "unknown"
}:

buildGoModule rec {
  pname = "pmx";
  version = "0.1.0";

  src = lib.cleanSource ../.;

  # subPackages builds only the root main; cmd/pmx-mocks is excluded intentionally.
  # Do not remove subPackages or the mock binary will also land in $out/bin.
  subPackages = [ "." ];

  vendorHash = "sha256-4R6iGmAj+njxRZ8HIImSuVmho0ZplXBZgfXUGA75ns0=";

  env.CGO_ENABLED = "0";

  ldflags = [
    "-s" "-w"
    "-X github.com/5Sigma/pmx/internal/version.Commit=${gitRev}"
  ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    wrapProgram $out/bin/pmx \
      --prefix PATH : ${lib.makeBinPath [ xdg-utils ]}

    install -Dm644 contrib/pmxd.service \
      $out/share/systemd/user/pmxd.service
  '';

  meta = with lib; {
    description = "Keyboard-driven terminal email client (pmx TUI + pmxd daemon)";
    homepage    = "https://github.com/5Sigma/pmx";
    license     = licenses.mit;
    mainProgram = "pmx";
    platforms   = platforms.unix;
    maintainers = [ ];
  };
}
