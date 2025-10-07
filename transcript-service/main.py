from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import Iterable

from fastapi.concurrency import run_in_threadpool  # Import run_in_threadpool
from youtube_transcript_api import (
    NoTranscriptFound,
    TranscriptsDisabled,
    VideoUnavailable,
    YouTubeTranscriptApi,
)

app = FastAPI()

LANGUAGE_PRIORITY = ["en", "en-US", "en-GB"]
TARGET_TRANSLATION_LANGUAGE = "en"
ytt_api = YouTubeTranscriptApi()

class VideoRequest(BaseModel):
    video_id: str

def _format_fetched_transcript(snippets: Iterable) -> str:
    """Convert a FetchedTranscript or iterable of snippets into a single string."""
    parts = []
    for snippet in snippets:
        text = getattr(snippet, "text", None)
        if text:
            parts.append(text.strip())
    return " ".join(filter(None, parts)).strip()

async def fetch_transcript_sync(video_id: str):
    """A synchronous wrapper for the blocking library call."""
    try:
        fetched_transcript = ytt_api.fetch(
            video_id,
            languages=LANGUAGE_PRIORITY,
        )
        return _format_fetched_transcript(fetched_transcript)
    except NoTranscriptFound as base_error:
        transcript_list = ytt_api.list(video_id)
        try:
            transcript = transcript_list.find_transcript(LANGUAGE_PRIORITY)
            return _format_fetched_transcript(transcript.fetch())
        except NoTranscriptFound:
            for transcript in transcript_list:
                if not transcript.is_translatable:
                    continue
                try:
                    fetched = transcript.translate(TARGET_TRANSLATION_LANGUAGE).fetch()
                except Exception:
                    continue
                formatted = _format_fetched_transcript(fetched)
                if formatted:
                    return formatted
            raise base_error
    # Let other specific exceptions be caught by the caller
    except (TranscriptsDisabled, VideoUnavailable) as e:
        raise e
    except Exception as e:
        # It's better to re-raise a generic exception for unexpected errors
        raise Exception(f"An unexpected error occurred in transcript fetching: {str(e)}")


@app.post("/get_transcript")
async def get_transcript(request: VideoRequest):
    """
    Accepts a video_id and returns the transcript for that video.
    """
    try:
        # Run the blocking function in a thread pool
        transcript_text = await run_in_threadpool(fetch_transcript_sync, request.video_id)
        return {"transcript": transcript_text}
    except (TranscriptsDisabled, VideoUnavailable, NoTranscriptFound) as e:
        raise HTTPException(status_code=404, detail=str(e))
    except Exception as e:
        # Catch the re-raised generic exception
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/health")
async def health_check():
    return {"status": "ok"}