from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from youtube_transcript_api import YouTubeTranscriptApi, TranscriptsDisabled, NoTranscriptFound, VideoUnavailable
from fastapi.concurrency import run_in_threadpool # Import run_in_threadpool

app = FastAPI()

class VideoRequest(BaseModel):
    video_id: str

async def fetch_transcript_sync(video_id: str):
    """A synchronous wrapper for the blocking library call."""
    try:
        # First attempt
        transcript_list = YouTubeTranscriptApi.get_transcript(video_id, languages=['en', 'en-US', 'en-GB'])
        return " ".join([seg['text'] for seg in transcript_list])
    except NoTranscriptFound:
        # Fallback attempt
        transcript_list = YouTubeTranscriptApi.list_transcripts(video_id)
        transcript = transcript_list.find_transcript(['en', 'en-US', 'en-GB']).fetch()
        return " ".join([seg['text'] for seg in transcript])
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