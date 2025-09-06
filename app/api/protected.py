from fastapi import APIRouter, Depends
from app.core.auth import current_active_user, current_superuser
from app.models.user import User

router = APIRouter()


@router.get("/protected-route")
async def protected_route(user: User = Depends(current_active_user)):
    """Example protected route that requires authentication."""
    return {
        "message": f"Hello {user.email}! This is a protected route.",
        "user_id": str(user.id),
        "user_data": {
            "email": user.email,
            "first_name": user.first_name,
            "last_name": user.last_name,
            "organization": user.organization,
            "total_compute_hours": user.total_compute_hours,
            "total_cost": user.total_cost,
        }
    }


@router.get("/admin-only")
async def admin_only_route(user: User = Depends(current_superuser)):
    """Example admin-only route."""
    return {
        "message": f"Hello admin {user.email}! This is an admin-only route.",
        "user_id": str(user.id),
    }
