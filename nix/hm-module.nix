self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.pmxd;
  isLinux  = pkgs.stdenv.isLinux;
  isDarwin = pkgs.stdenv.isDarwin;
  exe = lib.escapeShellArgs ([ "${cfg.package}/bin/pmxd" ] ++ cfg.extraArgs);

  # If the user has programs.password-store enabled with a non-default store
  # dir, forward PASSWORD_STORE_DIR into the service automatically so
  # password_command = "pass show ..." works without any extra config.
  passwordStoreEnv =
    let ps = config.programs.password-store; in
    lib.optionalAttrs
      (ps.enable or false && (ps.settings.PASSWORD_STORE_DIR or "") != "")
      { PASSWORD_STORE_DIR = ps.settings.PASSWORD_STORE_DIR; };

  # Explicit cfg.environment takes precedence over the auto-detected value.
  serviceEnv = passwordStoreEnv // cfg.environment;
in {
  options.services.pmxd = {
    enable = lib.mkEnableOption "pmxd, the pmx mail sync daemon";

    package = lib.mkOption {
      type        = lib.types.package;
      default     = self.packages.${pkgs.system}.pmxd;
      defaultText = lib.literalExpression "pmx.packages.\${pkgs.system}.pmxd";
      description = "The pmx package to use (provides both pmx and pmxd binaries).";
    };

    extraArgs = lib.mkOption {
      type        = lib.types.listOf lib.types.str;
      default     = [ ];
      example     = [ "--once" ];
      description = "Extra command-line arguments passed to pmxd.";
    };

    environment = lib.mkOption {
      type        = lib.types.attrsOf lib.types.str;
      default     = { };
      example     = { GNUPGHOME = "/home/user/.gnupg-work"; };
      description = ''
        Extra environment variables passed to the pmxd service, merged on top
        of any variables the module infers automatically (e.g.
        PASSWORD_STORE_DIR from programs.password-store).
      '';
    };

    autoStart = lib.mkOption {
      type        = lib.types.bool;
      default     = true;
      description = ''
        Whether pmxd starts automatically at login.
        Linux: sets Install.WantedBy = default.target.
        macOS: sets RunAtLoad = true in the launchd agent.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    # Both pmx and pmxd live in the same derivation; one package exposes both.
    home.packages = [ cfg.package ];

    # Linux: systemd user service
    systemd.user.services.pmxd = lib.mkIf isLinux {
      Unit = {
        Description = "pmx mail daemon";
        After  = [ "network-online.target" ];
        Wants  = [ "network-online.target" ];
      };

      Service = {
        Type       = "simple";
        ExecStart  = exe;
        Restart    = "on-failure";
        RestartSec = 10;
        Nice       = 10;
        IOSchedulingClass    = "best-effort";
        IOSchedulingPriority = 7;
      } // lib.optionalAttrs (serviceEnv != { }) {
        Environment = lib.mapAttrsToList (k: v: "${k}=${v}") serviceEnv;
      };

      Install = lib.mkIf cfg.autoStart {
        WantedBy = [ "default.target" ];
      };
    };

    # macOS: launchd user agent
    launchd.agents.pmxd = lib.mkIf isDarwin {
      enable = true;
      config = {
        Label           = "io.5sigma.pmxd";
        ProgramArguments = [ "${cfg.package}/bin/pmxd" ] ++ cfg.extraArgs;
        RunAtLoad       = cfg.autoStart;
        KeepAlive       = { Crashed = true; };
        ThrottleInterval = 10;
        Nice            = 10;
        LowPriorityIO   = true;
        StandardOutPath = "${config.home.homeDirectory}/Library/Logs/pmxd.log";
        StandardErrorPath = "${config.home.homeDirectory}/Library/Logs/pmxd.log";
        EnvironmentVariables = serviceEnv;
      };
    };
  };
}
