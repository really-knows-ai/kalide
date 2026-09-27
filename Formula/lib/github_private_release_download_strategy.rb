# frozen_string_literal: true

# Download strategy for `kalide`, which is distributed from the *private*
# GitHub repository really-knows-ai/kalide.
#
# The formula's `url` is the ordinary-looking release URL:
#
#   https://github.com/OWNER/REPO/releases/download/TAG/ASSET
#
# but a private repository cannot serve that URL anonymously. This strategy
# resolves the asset through the GitHub REST API instead:
#
#   1. it reads the user's token from HOMEBREW_GITHUB_API_TOKEN at fetch time
#      (not at formula-load time, so `brew info` stays usable without one);
#   2. it asks the API for the release identified by TAG and picks the asset
#      whose name matches ASSET (the formula's arch-specific
#      `kalide-darwin-arm64` or `kalide-darwin-amd64`);
#   3. CurlDownloadStrategy then downloads that asset API URL carrying
#        Authorization: Bearer <token>
#        Accept: application/octet-stream
#      and follows GitHub's redirect to the signed download URL.
#
# A missing or rejected token therefore fails with an explanatory message
# instead of a bare HTTP error.

require "download_strategy"
require "json"
require "net/http"
require "uri"

# Resolves a private GitHub release asset through the API using the user's
# HOMEBREW_GITHUB_API_TOKEN.
class GitHubPrivateReleaseDownloadStrategy < CurlDownloadStrategy
  # Add the headers every request needs. The token is read (not validated) so
  # a missing token is reported with a clear message when the download starts,
  # rather than when Homebrew merely loads the formula.
  def initialize(url, name, version, **meta)
    headers = Array(meta[:headers]).dup
    headers << "Authorization: Bearer #{ENV.fetch("HOMEBREW_GITHUB_API_TOKEN", "").strip}"
    headers << "Accept: application/octet-stream"
    meta[:headers] = headers

    super

    parse_url_pattern
  end

  # Homebrew calls this to fetch the artifact. Point @url at the API asset URL
  # for the private download and let CurlDownloadStrategy do the transfer (it
  # already carries the auth headers added in #initialize). Deferring the API
  # lookup to fetch time keeps `brew info` offline and token-free.
  def fetch(*args, **options, &block)
    original_url = @url
    @url = asset_api_url
    super
  ensure
    @url = original_url
  end

  # Name the staged/downloaded file after the release asset rather than the
  # numeric API asset id, so the formula can install the arch-specific asset
  # (`kalide-darwin-arm64` or `kalide-darwin-amd64`).
  def resolved_basename
    @asset_name
  end

  private

  # Capture the owner/repo/tag/asset parts of the release-download URL form.
  # Anything else is a formula bug, so fail loudly.
  def parse_url_pattern
    pattern = %r{\Ahttps://github\.com/(?<owner>[^/]+)/(?<repo>[^/]+)/releases/download/(?<tag>[^/?#]+)/(?<asset>[^/?#]+)\z}
    match = pattern.match(@url.to_s)
    unless match
      raise CurlDownloadStrategyError,
            "Invalid private-release URL (expected " \
            "https://github.com/OWNER/REPO/releases/download/TAG/ASSET): #{@url}"
    end

    @gh_owner = match[:owner]
    @gh_repo = match[:repo]
    @gh_tag = match[:tag]
    @asset_name = match[:asset]
  end

  # The user's GitHub token. kalide ships from a private repository, so there is
  # no anonymous download path.
  def token
    @token ||= begin
      value = ENV.fetch("HOMEBREW_GITHUB_API_TOKEN", "").strip

      if value.empty?
        raise CurlDownloadStrategyError, <<~EOS
          HOMEBREW_GITHUB_API_TOKEN is not set, so kalide cannot be downloaded.

          kalide is distributed from the private GitHub repository
          #{@gh_owner}/#{@gh_repo}. Create a GitHub token that can read it and
          export it before installing:

            export HOMEBREW_GITHUB_API_TOKEN=<your token>

          See INSTALL.md for the full walk-through.
        EOS
      end

      value
    end
  end

  # Ask the API for the release by tag and return the API URL of the named
  # asset. That asset URL is what accepts `Accept: application/octet-stream`.
  def asset_api_url
    @asset_api_url ||= begin
      release = github_json("https://api.github.com/repos/#{@gh_owner}/#{@gh_repo}/releases/tags/#{@gh_tag}")
      assets = release["assets"]
      assets = [] unless assets.is_a?(Array)
      asset = assets.find { |candidate| candidate["name"] == @asset_name }
      asset_url = asset && asset["url"]

      unless asset_url
        raise CurlDownloadStrategyError,
              "GitHub release #{@gh_tag} of #{@gh_owner}/#{@gh_repo} has no asset named " \
              "#{@asset_name}."
      end

      asset_url
    end
  end

  def github_json(url)
    response = github_request(url, "application/vnd.github+json")

    case response
    when Net::HTTPSuccess
      begin
        JSON.parse(response.body)
      rescue JSON::ParserError
        raise CurlDownloadStrategyError,
              "GitHub returned an unexpected response for #{url} (HTTP #{response.code}); " \
              "check HOMEBREW_GITHUB_API_TOKEN."
      end
    when Net::HTTPUnauthorized, Net::HTTPForbidden
      raise CurlDownloadStrategyError, <<~EOS
        GitHub rejected HOMEBREW_GITHUB_API_TOKEN (HTTP #{response.code}).

        The token must be able to read the private repository
        #{@gh_owner}/#{@gh_repo}. Check that it is valid and has not expired,
        then export it again:

          export HOMEBREW_GITHUB_API_TOKEN=<your token>
      EOS
    when Net::HTTPNotFound
      raise CurlDownloadStrategyError,
            "GitHub release #{@gh_tag} was not found in #{@gh_owner}/#{@gh_repo} (HTTP 404)."
    else
      raise CurlDownloadStrategyError,
            "GitHub API request for #{url} failed (HTTP #{response.code})."
    end
  end

  def github_request(url, accept)
    uri = URI.parse(url)
    request = Net::HTTP::Get.new(uri)
    request["Authorization"] = "Bearer #{token}"
    request["Accept"] = accept
    request["X-GitHub-Api-Version"] = "2022-11-28"
    request["User-Agent"] = "Homebrew"

    Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https") do |http|
      http.request(request)
    end
  rescue SocketError, SystemCallError, Timeout::Error, IOError => e
    raise CurlDownloadStrategyError,
          "Could not reach the GitHub API to download kalide: #{e.message}"
  end
end
