{
  description = "Noket user client: the noket CLI and the noketd tunnel daemon.";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
    ...
  }: let
    version =
      if (self ? shortRev)
      then self.shortRev
      else "dev";

    buildNoket = {pkgs}: (pkgs.buildGoModule.override {go = pkgs.go_1_27;}) rec {
      pname = "noket";
      inherit version;
      src = pkgs.lib.cleanSource self;

      vendorHash = "sha256-ZewweW6z0S+F5OKe52cjGk/7NXJ+M3FeNw1kBu1yVmk=";

      subPackages = ["cmd/noket" "cmd/noketd"];

      ldflags = ["-s" "-w" "-X main.version=v${version}"];
    };
  in
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = import nixpkgs {inherit system;};
      noket = buildNoket {inherit pkgs;};
      devDeps =
        (with pkgs; [git go_1_27 gnumake])
        ++ (with pkgs; [
          buf
          golangci-lint
          gofumpt
          goreleaser
          protobuf
          protoc-gen-connect-go
          protoc-gen-go
          syft
        ]);
    in {
      packages = {
        default = noket;
        inherit noket;
      };

      checks.default = noket;

      devShells.default = pkgs.mkShell {
        buildInputs = devDeps;
      };
    });
}
