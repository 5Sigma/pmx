{ pkgs, lib, config, inputs, ... }:

{
  packages = with pkgs; [
    gopls
    gotools        # goimports, godoc, stringer
    golangci-lint
    delve          # debugger (dlv)
    air            # live reload
    go-migrate     # db migrations (for local cache store)
    sqlite         # local message cache
  ];

  languages.go = {
    enable = true;
  };

  scripts = {
    build.exec = "go build -o ./bin/pmx .";
    dev.exec   = "air";
    lint.exec  = "golangci-lint run ./...";
    test.exec  = "go test -v -race ./...";
    tidy.exec  = "go mod tidy";
    gen.exec   = "go generate ./...";
    mocks.exec = "go run ./cmd/pmx-mocks";
  };

  processes.mock-mail.exec = "go run ./cmd/pmx-mocks";

  enterShell = ''
    echo ""
    echo "  Phreakmx (pmx)"
    echo "  $(go version)"
    echo ""
    echo "  build  compile to ./bin/pmx"
    echo "  dev    live reload with air"
    echo "  lint   golangci-lint"
    echo "  test   go test -race"
    echo "  tidy   go mod tidy"
    echo ""
    if [ ! -f go.sum ]; then
      echo "  note: run 'tidy' to fetch dependencies"
      echo ""
    fi
  '';

  pre-commit.hooks = {
    gofmt.enable         = true;
    golangci-lint.enable = true;
  };
}
