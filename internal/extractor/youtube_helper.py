import sys
import json
import os
import subprocess
import importlib.util
from youtube_transcript_api import YouTubeTranscriptApi, TranscriptsDisabled, NoTranscriptFound, VideoUnavailable
from youtube_transcript_api.proxies import WebshareProxyConfig

def main():
    """
    A long-lived process that listens for video ID requests on stdin
    and returns transcript data on stdout.
    """
    # Initialize the API, using a proxy if credentials are provided
    username = os.getenv('WEBSHARE_PROXY_USERNAME')
    password = os.getenv('WEBSHARE_PROXY_PASSWORD')
    if username and password:
        api = YouTubeTranscriptApi(proxy_config=WebshareProxyConfig(proxy_username=username, proxy_password=password))
    else:
        api = YouTubeTranscriptApi()

    # Main loop to process requests
    for line in sys.stdin:
        try:
            request = json.loads(line)
            video_id = request.get("video_id")

            if not video_id:
                print(json.dumps({"error": "No video_id provided"}), flush=True)
                continue

            try:
                # Attempt to fetch the transcript in English
                transcript_list = api.get_transcript(video_id, languages=['en', 'en-US', 'en-GB'])
                transcript_text = " ".join([seg['text'] for seg in transcript_list])
                print(json.dumps({"transcript": transcript_text}), flush=True)
            except NoTranscriptFound:
                # Fallback to any available transcript if English is not found
                transcript_list = api.list_transcripts(video_id)
                transcript = transcript_list.find_transcript(['en', 'en-US', 'en-GB']).fetch()
                transcript_text = " ".join([seg['text'] for seg in transcript])
                print(json.dumps({"transcript": transcript_text}), flush=True)
            except (TranscriptsDisabled, VideoUnavailable, NoTranscriptFound) as e:
                print(json.dumps({"error": str(e)}), flush=True)

        except json.JSONDecodeError:
            print(json.dumps({"error": "Invalid JSON input"}), flush=True)
        except Exception as e:
            print(json.dumps({"error": f"An unexpected error occurred: {str(e)}"}), flush=True)

if __name__ == "__main__":
    main()