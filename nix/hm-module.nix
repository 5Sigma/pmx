self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.pmxd;
  isLinux  = pkgs.stdenv.isLinux;
  isDarwin = pkgs.stdenv.isDarwin;
  exe = lib.escapeShellArgs ([ "${cfg.package}/bin/pmxd" ] ++ cfg.extraArgs);
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
      };
    };
  };
}
