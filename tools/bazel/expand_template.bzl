load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")

def _expand_template_impl(ctx):
    substitutions = dict(ctx.attr.static_substitutions)
    for key, target in ctx.attr.substitutions.items():
        substitutions[key] = target[BuildSettingInfo].value

    ctx.actions.expand_template(
        template = ctx.file.template,
        output = ctx.outputs.out,
        substitutions = substitutions,
    )

expand_template = rule(
    implementation = _expand_template_impl,
    attrs = {
        "out": attr.output(mandatory = True),
        "static_substitutions": attr.string_dict(),
        "substitutions": attr.string_keyed_label_dict(
            providers = [BuildSettingInfo],
        ),
        "template": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
    },
)
